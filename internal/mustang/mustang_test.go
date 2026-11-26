package mustang

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
)

func open(t testing.TB) *Store {
	s, err := Open(badger.DefaultOptions("").WithInMemory(true).WithLogger(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSmoke(t *testing.T) {
	s := open(t)
	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.UpsertTable("users", Table{"a": {"name": "x", "age": 3.0}, "b": {"ok": true, "at": now}}))
	must(s.UpsertValue("users2", "z", "name", "y"))
	tabs, _ := s.ScanTables()
	rows, _ := s.ScanRows("users")
	cols, _ := s.ScanColumns("users")
	t.Log(tabs, rows, cols)
	if len(tabs) != 2 || len(rows) != 2 || len(cols) != 4 {
		t.Fatal("scan")
	}
	v, err := s.GetValue("users", "b", "at")
	must(err)
	if !v.(time.Time).Equal(now) {
		t.Fatal("time")
	}
	col, _ := s.GetColumn("users", "name")
	if len(col) != 1 {
		t.Fatal("col")
	}
	must(s.DeleteColumn("users", "name"))
	must(s.DeleteRow("users", "b"))
	tab, err := s.GetTable("users")
	must(err)
	t.Log(tab)
	if len(tab) != 1 {
		t.Fatal("tab")
	}
	must(s.DeleteTable("users"))
	if _, err := s.GetRow("users", "a"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := s.UpsertValue("bad name", "r", "c", "v"); err == nil {
		t.Fatal("name")
	}
	if err := s.UpsertValue("t", "r", "c", 1); err == nil {
		t.Fatal("type")
	}
}

func TestBatch(t *testing.T) {
	s := open(t)
	err := s.Batch().
		Set("t", "a", "x", 1.0).
		Set("t", "a", "y", "s").
		Set("t", "b", "x", true).
		Set("u", "a", "x", 2.0).
		DeleteRow("t", "b").
		Delete("t", "a", "y").
		Commit()
	if err != nil {
		t.Fatal(err)
	}
	vs, err := s.GetValues([]Cell{{"t", "a", "x"}, {"t", "a", "y"}, {"t", "b", "x"}, {"u", "a", "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if vs[0] != 1.0 || vs[1] != nil || vs[2] != nil || vs[3] != 2.0 {
		t.Fatal(vs)
	}
	if err := s.Batch().DeleteTable("u").Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTable("u"); err != ErrNotFound {
		t.Fatal(err)
	}
	// An invalid op fails the whole batch; nothing is written.
	if err := s.Batch().Set("t", "z", "x", 1.0).Set("t", "z", "y", 1).Commit(); err == nil {
		t.Fatal("type")
	}
	if _, err := s.GetRow("t", "z"); err != ErrNotFound {
		t.Fatal("partial write")
	}
}

func TestQuery(t *testing.T) {
	s := open(t)
	err := s.UpsertTable("u", Table{
		"eu-1": {"name": "ann", "age": 30.0, "admin": true},
		"eu-2": {"name": "bob", "age": 12.0},
		"eu-3": {"name": "cid", "age": 40.0},
		"us-1": {"name": "dan", "age": 50.0},
		"us-2": {"name": "eve"},
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := func(rs []Result) (ks []string) {
		for _, r := range rs {
			ks = append(ks, r.Key)
		}
		return ks
	}
	cases := []struct {
		q    *Query
		want string
	}{
		{s.Query("u").Where("age", Ge, 18.0), "[eu-1 eu-3 us-1]"},
		{s.Query("u").Prefix("eu-").Where("age", Ge, 18.0), "[eu-1 eu-3]"},
		{s.Query("u").From("eu-2").To("us-2"), "[eu-2 eu-3 us-1]"},
		{s.Query("u").Where("age", Gt, 10.0).Limit(2), "[eu-1 eu-2]"},
		{s.Query("u").Where("admin", Exists, nil), "[eu-1]"},
		{s.Query("u").Where("name", Prefix, "e"), "[us-2]"},
		{s.Query("u").Where("age", Ne, 30.0), "[eu-2 eu-3 us-1]"},
		{s.Query("u").Where("age", Eq, "30"), "[]"},
		{s.Query("u").Prefix("zz"), "[]"},
	}
	for i, c := range cases {
		rs, err := c.q.Run()
		if err != nil {
			t.Fatal(i, err)
		}
		if got := "[" + strings.Join(keys(rs), " ") + "]"; got != c.want {
			t.Errorf("case %d: got %s want %s", i, got, c.want)
		}
	}
	rs, err := s.Query("u").Where("age", Lt, 35.0).Select("name").Run()
	if err != nil || len(rs) != 2 || len(rs[0].Row) != 1 || rs[0].Row["name"] != "ann" {
		t.Fatal(rs, err)
	}
	if _, err := s.Query("u").Where("age", Eq, 1).Run(); err == nil {
		t.Fatal("type")
	}
}

func TestIf(t *testing.T) {
	s := open(t)
	// Create only if absent.
	if err := s.Batch().If("a", "x", "v", Missing, nil).Set("a", "x", "v", 1.0).Commit(); err != nil {
		t.Fatal(err)
	}
	err := s.Batch().If("a", "x", "v", Missing, nil).Set("a", "x", "v", 9.0).Commit()
	if !errors.Is(err, ErrCondition) {
		t.Fatal(err)
	}
	// Compare-and-set.
	if err := s.Batch().If("a", "x", "v", Eq, 1.0).Set("a", "x", "v", 2.0).Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.Batch().If("a", "x", "v", Eq, 1.0).Set("a", "x", "v", 3.0).Commit(); !errors.Is(err, ErrCondition) {
		t.Fatal(err)
	}
	// Compare-and-delete.
	if err := s.Batch().If("a", "x", "v", Eq, 2.0).DeleteRow("a", "x").Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetValue("a", "x", "v"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestIncr(t *testing.T) {
	s := open(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 10 {
				if _, err := s.Incr("c", "hits", "n", 1); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	v, err := s.GetValue("c", "hits", "n")
	if err != nil || v != 200.0 {
		t.Fatal(v, err)
	}
	s.UpsertValue("c", "s", "n", "str")
	if _, err := s.Incr("c", "s", "n", 1); !errors.Is(err, ErrType) {
		t.Fatal(err)
	}
	if err := s.Batch().Incr("c", "hits", "n", -50).Commit(); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetValue("c", "hits", "n"); v != 150.0 {
		t.Fatal(v)
	}
}

func TestAgg(t *testing.T) {
	s := open(t)
	s.UpsertTable("u", Table{
		"a": {"age": 30.0, "name": "ann"},
		"b": {"age": 10.0, "name": "bob", "admin": true},
		"c": {"age": 50.0},
		"d": {"name": "dan"},
	})
	if n, _ := s.Query("u").Count(); n != 4 {
		t.Fatal(n)
	}
	if n, _ := s.Query("u").Where("admin", Missing, nil).Count(); n != 3 {
		t.Fatal(n)
	}
	if n, _ := s.Query("u").Limit(2).Count(); n != 2 {
		t.Fatal(n)
	}
	if v, _ := s.Query("u").Sum("age"); v != 90.0 {
		t.Fatal(v)
	}
	if v, _ := s.Query("u").Avg("age"); v != 30.0 {
		t.Fatal(v)
	}
	if v, _ := s.Query("u").Where("age", Gt, 10.0).Min("age"); v != 30.0 {
		t.Fatal(v)
	}
	if v, _ := s.Query("u").Max("name"); v != "dan" {
		t.Fatal(v)
	}
	if _, err := s.Query("u").Sum("name"); !errors.Is(err, ErrType) {
		t.Fatal(err)
	}
	if _, err := s.Query("u").Avg("nope"); err != ErrNotFound {
		t.Fatal(err)
	}
	rs, _ := s.Query("u").Where("age", Missing, nil).Run()
	if len(rs) != 1 || rs[0].Key != "d" {
		t.Fatal(rs)
	}
}

func BenchmarkQuery(b *testing.B) {
	s := open(b)
	tab := Table{}
	for i := range 2000 {
		row := Row{"age": float64(i % 100), "name": "x"}
		for j := range 20 {
			row[fmt.Sprintf("c%02d", j)] = "pad"
		}
		tab[fmt.Sprintf("r%05d", i)] = row
	}
	if err := s.UpsertTable("t", tab); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Query("t").Where("age", Ge, 50.0).Select("name").Run(); err != nil {
			b.Fatal(err)
		}
	}
}
