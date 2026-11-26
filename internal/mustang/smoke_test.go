package mustang

import (
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
)

func TestSmoke(t *testing.T) {
	s, err := Open(badger.DefaultOptions("").WithInMemory(true).WithLogger(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
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
	s, err := Open(badger.DefaultOptions("").WithInMemory(true).WithLogger(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Batch().
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
