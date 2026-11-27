package mustang

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"

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

func load(t testing.TB, s *Store, name string, tab Table) {
	b := s.Batch()
	for r, row := range tab {
		for c, v := range row {
			b.Set(name, r, c, v)
		}
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestSmoke(t *testing.T) {
	s := open(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	load(t, s, "users", Table{"a": {"name": "x", "age": 3.0}, "b": {"ok": true, "at": "2026-11-26T10:00:00Z"}})
	must(s.Batch().Set("users2", "z", "name", "y").Commit())
	tabs, _ := s.ScanTables()
	rows, _ := s.ScanRows("users")
	cols, _ := s.ScanColumns("users")
	t.Log(tabs, rows, cols)
	if len(tabs) != 2 || len(rows) != 2 || len(cols) != 4 {
		t.Fatal("scan")
	}
	v, err := s.GetValue("users", "b", "at")
	must(err)
	if v != "2026-11-26T10:00:00Z" {
		t.Fatal("at", v)
	}
	col, _ := s.GetColumn("users", "name")
	if len(col) != 1 {
		t.Fatal("col")
	}
	must(s.Batch().DeleteColumn("users", "name").DeleteRow("users", "b").Commit())
	tab, err := s.GetTable("users")
	must(err)
	t.Log(tab)
	if len(tab) != 1 {
		t.Fatal("tab")
	}
	must(s.Batch().DeleteTable("users").Commit())
	if _, err := s.GetRow("users", "a"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := s.Batch().Set("bad name", "r", "c", "v").Commit(); err == nil {
		t.Fatal("name")
	}
	if err := s.Batch().Set("t", "r", "c", 1).Commit(); err == nil {
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
	load(t, s, "u", Table{
		"eu-1": {"name": "ann", "age": 30.0, "admin": true},
		"eu-2": {"name": "bob", "age": 12.0},
		"eu-3": {"name": "cid", "age": 40.0},
		"us-1": {"name": "dan", "age": 50.0},
		"us-2": {"name": "eve"},
	})
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
		{s.Query("u").Where("name", Contains, "a"), "[eu-1 us-1]"},
		{s.Query("u").Where("name", Contains, "A"), "[]"},
		{s.Query("u").Where("age", Contains, "3"), "[]"},
		{s.Query("u").Where("name", Contains, "o").Select("age"), "[eu-2]"},
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

// TestSelect checks that, as in Bigtable, rows without any selected
// column are dropped, and that limit counts only rows returned.
func TestSelect(t *testing.T) {
	s := open(t)
	load(t, s, "u", Table{
		"a": {"name": "ann"},
		"b": {"age": 10.0},
		"c": {"name": "cid", "age": 30.0},
		"d": {"name": "dan"},
	})
	cases := []struct {
		q    *Query
		want string
	}{
		{s.Query("u").Select("age"), "[b c]"},
		{s.Query("u").Select("age", "name"), "[a b c d]"},
		{s.Query("u").Select("nope"), "[]"},
		{s.Query("u").Select("name").Limit(2), "[a c]"},
		{s.Query("u").Where("name", Exists, nil).Select("age"), "[c]"},
	}
	for i, c := range cases {
		rs, err := c.q.Run()
		if err != nil {
			t.Fatal(i, err)
		}
		var ks []string
		for _, r := range rs {
			ks = append(ks, r.Key)
		}
		if got := "[" + strings.Join(ks, " ") + "]"; got != c.want {
			t.Errorf("case %d: got %s want %s", i, got, c.want)
		}
	}
	if n, _ := s.Query("u").Select("age").Count(); n != 4 {
		t.Fatal("count ignores select", n)
	}
	if v, _ := s.Query("u").Limit(1).Sum("age"); v != 10.0 {
		t.Fatal("limit counts rows with the column", v)
	}
}

func TestIf(t *testing.T) {
	s := open(t)
	// Create only if absent.
	if err := s.Batch().If("a", "x", "v", Missing, nil).Set("a", "x", "v", 1.0).Commit(); err != nil {
		t.Fatal(err)
	}
	err := s.Batch().If("a", "x", "v", Missing, nil).Set("a", "x", "v", 9.0).Commit()
	if !errors.Is(err, ErrCondition) || err.Error() != "mustang: condition failed: a/x/v" {
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
	s.Batch().Set("c", "s", "n", "str").Commit()
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
	load(t, s, "u", Table{
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

func TestIfContains(t *testing.T) {
	s := open(t)
	s.Batch().Set("a", "x", "tags", "red,green").Commit()
	if err := s.Batch().If("a", "x", "tags", Contains, "green").Set("a", "x", "ok", true).Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.Batch().If("a", "x", "tags", Contains, "blue").Commit(); !errors.Is(err, ErrCondition) {
		t.Fatal(err)
	}
	if err := s.Batch().If("a", "x", "tags", Contains, 1.0).Commit(); !errors.Is(err, ErrType) {
		t.Fatal(err)
	}
}

// TestOrder checks that encodings order like their values, as
// conditions and Min/Max compare them raw.
func TestOrder(t *testing.T) {
	vals := [][]Value{
		{math.Inf(-1), -1e9, -1.5, -0.25, 0.0, 1e-300, 0.25, 2.0, 1e9, math.Inf(1)},
		{false, true},
		{"", "a", "a\x00", "ab", "b"},
	}
	for _, vs := range vals {
		for i := range vs {
			a, _ := encode(vs[i])
			if v, err := decode(a); err != nil || v != vs[i] {
				t.Errorf("roundtrip %v: %v %v", vs[i], v, err)
			}
			if i == 0 {
				continue
			}
			p, _ := encode(vs[i-1])
			if bytes.Compare(p, a) >= 0 {
				t.Errorf("%v sorts after %v", vs[i-1], vs[i])
			}
		}
	}
	a, _ := encode(math.Copysign(0, -1))
	b, _ := encode(0.0)
	if !bytes.Equal(a, b) {
		t.Error("-0 != 0")
	}
	if _, err := encode(math.NaN()); !errors.Is(err, ErrType) {
		t.Error("NaN")
	}
}

// TestReverse checks that a reverse scan returns exactly the rows of
// a forward scan, backwards. Rows are wide enough that next seeks past
// them, and keys share prefixes ("a" < "a0" < "ab") and a neighbouring
// table ("r2") so the bounds are exercised.
func TestReverse(t *testing.T) {
	s := open(t)
	tab := Table{}
	for _, k := range []string{"a", "a0", "a1", "ab", "b", "b5", "c", "c\xff", "d"} {
		row := Row{"n": float64(len(k))}
		for j := range 2 * seekAfter {
			row[fmt.Sprintf("c%02d", j)] = k
		}
		tab[k] = row
	}
	delete(tab["b"], "n")
	load(t, s, "r", tab)
	load(t, s, "r2", Table{"a": {"n": 1.0}, "z": {"n": 1.0}})
	load(t, s, "q", Table{"zz": {"n": 1.0}})

	qs := []func() *Query{
		func() *Query { return s.Query("r") },
		func() *Query { return s.Query("r").Prefix("a") },
		func() *Query { return s.Query("r").Prefix("c") },
		func() *Query { return s.Query("r").Prefix("zz") },
		func() *Query { return s.Query("r").From("a1").To("c") },
		func() *Query { return s.Query("r").From("a1") },
		func() *Query { return s.Query("r").To("b") },
		func() *Query { return s.Query("r").To("b5") },
		func() *Query { return s.Query("r").Prefix("a").To("a1") },
		func() *Query { return s.Query("r").Prefix("b").From("a") },
		func() *Query { return s.Query("r").Prefix("a").From("b") },
		func() *Query { return s.Query("r").Where("n", Ge, 2.0) },
		func() *Query { return s.Query("r").Where("n", Missing, nil) },
		func() *Query { return s.Query("r").Select("n") },
		func() *Query { return s.Query("r").Select("c03", "n").Where("c05", Prefix, "a") },
	}
	for i, q := range qs {
		fw, err := q().Run()
		if err != nil {
			t.Fatal(i, err)
		}
		rv, err := q().Reverse().Run()
		if err != nil {
			t.Fatal(i, err)
		}
		slices.Reverse(rv)
		if fmt.Sprint(fw) != fmt.Sprint(rv) {
			t.Errorf("case %d:\n fw %v\n rv %v", i, fw, rv)
		}
		if len(fw) == 0 {
			continue
		}
		last, err := q().Reverse().Limit(1).Run()
		if err != nil || len(last) != 1 || last[0].Key != fw[len(fw)-1].Key {
			t.Errorf("case %d: last %v %v", i, last, err)
		}
	}
	if n, _ := s.Query("r").Reverse().Count(); n != len(tab) {
		t.Error("count", n)
	}
	if v, _ := s.Query("r").Reverse().Max("n"); v != 2.0 {
		t.Error("max", v)
	}
}

// TestTimeRange checks the timestamp convention: fixed-width UTC
// strings order like the instants, and a cut-off timestamp is a bound.
func TestTimeRange(t *testing.T) {
	s := open(t)
	load(t, s, "m", Table{
		"s1/2026-11-25T23:59:59Z": {"at": "2026-11-25T23:59:59Z"},
		"s1/2026-11-26T00:00:00Z": {"at": "2026-11-26T00:00:00Z"},
		"s1/2026-11-26T12:30:00Z": {"at": "2026-11-26T12:30:00Z"},
		"s1/2026-11-27T00:00:00Z": {"at": "2026-11-27T00:00:00Z"},
		"s2/2026-11-26T08:00:00Z": {"at": "2026-11-26T08:00:00Z"},
	})
	cases := []struct {
		q    *Query
		want int
	}{
		{s.Query("m").From("s1/2026-11-26").To("s1/2026-11-27"), 2},
		{s.Query("m").Prefix("s1/2026-11"), 4},
		{s.Query("m").Where("at", Ge, "2026-11-26").Where("at", Lt, "2026-11-27"), 3},
		{s.Query("m").Prefix("s1/").Reverse().Limit(1).Where("at", Lt, "2026-11-27"), 1},
	}
	for i, c := range cases {
		if n, err := c.q.Count(); err != nil || n != c.want {
			t.Errorf("case %d: %d %v", i, n, err)
		}
	}
	rs, err := s.Query("m").Prefix("s1/").Reverse().Limit(1).Where("at", Lt, "2026-11-27").Run()
	if err != nil || len(rs) != 1 || rs[0].Key != "s1/2026-11-26T12:30:00Z" {
		t.Fatal("latest", rs, err)
	}
}

func BenchmarkContains(b *testing.B) {
	s := open(b)
	tab := Table{}
	for i := range 20000 {
		tab[fmt.Sprintf("r%05d", i)] = Row{"name": fmt.Sprintf("user-%05d-somewhat-long-name", i)}
	}
	load(b, s, "t", tab)
	b.ReportAllocs()
	for b.Loop() {
		n, err := s.Query("t").Where("name", Contains, "00042").Count()
		if err != nil || n != 1 {
			b.Fatal(n, err)
		}
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
	load(b, s, "t", tab)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Query("t").Where("age", Ge, 50.0).Select("name").Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCount(b *testing.B) {
	s := open(b)
	tab := Table{}
	for i := range 20000 {
		tab[fmt.Sprintf("r%05d", i)] = Row{"age": float64(i % 100), "at": fmt.Sprintf("t%05d", i)}
	}
	load(b, s, "t", tab)
	b.ReportAllocs()
	for b.Loop() {
		n, err := s.Query("t").Where("age", Ge, 50.0).Where("at", Gt, "t00100").Count()
		if err != nil || n == 0 {
			b.Fatal(n, err)
		}
	}
}

// BenchmarkLatest reads the newest row of one series among many,
// the common time-range query that Reverse exists for.
func BenchmarkLatest(b *testing.B) {
	s := open(b)
	tab := Table{}
	for i := range 20000 {
		tab[fmt.Sprintf("s%02d/%05d", i%10, i)] = Row{"v": float64(i), "name": "x"}
	}
	load(b, s, "t", tab)
	b.ReportAllocs()
	for b.Loop() {
		rs, err := s.Query("t").Prefix("s03/").Reverse().Limit(1).Run()
		if err != nil || len(rs) != 1 || rs[0].Key != "s03/19993" {
			b.Fatal(rs, err)
		}
	}
}
