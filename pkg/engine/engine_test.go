package engine_test

import (
	"testing"

	"github.com/TheDevtop/quicktable/pkg/dkey"
	"github.com/TheDevtop/quicktable/pkg/engine"
	"github.com/dgraph-io/badger/v4"
)

func startEngine() (*badger.DB, error) {
	return badger.Open(badger.DefaultOptions("").WithInMemory(true))
}

func stopEngine(dbPtr *badger.DB) error {
	return dbPtr.Close()
}

func TestInsert(t *testing.T) {
	var (
		err error
		key string
		ptr *badger.DB
	)

	if ptr, err = startEngine(); err != nil {
		t.Fatal(err)
	}

	if key, err = engine.Insert(ptr, "foo:bar:baz", "content"); err != nil {
		t.Fatal(err)
	} else if key != "foo:bar:baz" {
		t.Fail()
	}

	if key, err = engine.Index(ptr, key); err != nil {
		t.Fatal(err)
	} else if key != "foo:bar:baz" {
		t.Fail()
	}

	if err = stopEngine(ptr); err != nil {
		t.Fatal(err)
	}
}

func TestInsertSelected(t *testing.T) {
	var (
		err    error
		result string
		ptr    *badger.DB
	)

	if ptr, err = startEngine(); err != nil {
		t.Fatal(err)
	}

	if err = engine.InsertSelected(ptr, "testing", map[string]string{
		"lorem": "0",
		"ipsum": "1",
		"sit":   "2",
		"dolor": "3",
		"amet":  "4",
	}); err != nil {
		t.Fatal(err)
	}

	if result, err = engine.Index(ptr, "testing:lorem"); err != nil || result != "testing:lorem" {
		t.Log(result, err)
		t.Fail()
	}

	if err = stopEngine(ptr); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteSelected(t *testing.T) {
	var (
		err error
		ptr *badger.DB
	)

	if ptr, err = startEngine(); err != nil {
		t.Fatal(err)
	}

	if err = engine.InsertSelected(ptr, "testing", map[string]string{
		"lorem": "0",
		"ipsum": "1",
		"sit":   "2",
		"dolor": "3",
		"amet":  "4",
	}); err != nil {
		t.Fatal(err)
	}

	if err = engine.DeleteSelected(ptr, "testing", []string{"sit", "amet"}); err != nil {
		t.Fatal(err)
	}

	if _, err := engine.Index(ptr, "testing:amet"); err == nil {
		t.Fatal("The key is still in the database")
	}

	if err = stopEngine(ptr); err != nil {
		t.Fatal(err)
	}
}

func TestQuerySelected(t *testing.T) {
	var (
		err    error
		result map[string]string
		ptr    *badger.DB
	)

	if ptr, err = startEngine(); err != nil {
		t.Fatal(err)
	}

	if err = engine.InsertSelected(ptr, "testing", map[string]string{
		"lorem": "0",
		"ipsum": "1",
		"sit":   "2",
		"dolor": "3",
		"amet":  "4",
	}); err != nil {
		t.Fatal(err)
	}

	if result, err = engine.QuerySelected(ptr, "testing", []string{"ipsum", "dolor", "amet"}); err != nil {
		t.Fatal(err)
	}

	if val, ok := result["dolor"]; !ok || val != "3" {
		t.Fail()
	}
	if _, ok := result["lorem"]; ok {
		t.Fail()
	}

	if err = stopEngine(ptr); err != nil {
		t.Fatal(err)
	}
}

func TestQueryPrefixed(t *testing.T) {
	var (
		err     error
		ptr     *badger.DB
		pairsIn = map[string]string{
			"lorem": "0",
			"ipsum": "1",
			"sit":   "2",
			"dolor": "3",
			"amet":  "4",
		}
		pairsOut map[string]string
	)

	if ptr, err = startEngine(); err != nil {
		t.Fatal(err)
	}

	if err = engine.InsertSelected(ptr, "testing", pairsIn); err != nil {
		t.Fatal(err)
	}

	if pairsOut, err = engine.QueryPrefixed(ptr, "testing"); err != nil {
		t.Fatal(err)
	}

	for k := range pairsIn {
		if _, o := pairsOut[dkey.Fuse("testing", k)]; !o {
			t.Logf("%v\n", pairsOut)
			t.Fail()
		}
	}

	if err = stopEngine(ptr); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateHash(t *testing.T) {
	if hash, err := engine.GenerateHash("tesing"); err != nil {
		t.Fatal(err)
	} else {
		t.Log(hash)
	}
}

func TestFn(t *testing.T) {
	var (
		err error
		ptr *badger.DB
	)

	if ptr, err = startEngine(); err != nil {
		t.Fatal(err)
	}

	// Put testing code here

	if err = stopEngine(ptr); err != nil {
		t.Fatal(err)
	}
}
