package flow

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunCheckCleanRepository(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("check me please"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	res, err := runCheck(ident, backend)
	if err != nil {
		t.Fatalf("runCheck() error: %v", err)
	}
	if res.Snapshots != 1 || len(res.Missing) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.DataObjects < 1 {
		t.Fatalf("DataObjects = %d", res.DataObjects)
	}
}

func TestRunCheckReportsMissingObject(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("check me please"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	keys := backend.KeysWithPrefix("data/")
	if len(keys) == 0 {
		t.Fatal("no data objects")
	}
	backend.Delete(keys[0])

	_, err := runCheck(ident, backend)
	if err == nil {
		t.Fatal("runCheck() succeeded with missing object")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v, want missing mention", err)
	}
}

func TestRunCheckReportsGarbageWithoutError(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("check me please"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	_ = backend.Upload("data/ff/ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", bytes.NewReader([]byte("orphan")))

	res, err := runCheck(ident, backend)
	if err != nil {
		t.Fatalf("runCheck() error: %v", err)
	}
	if len(res.Garbage) != 1 {
		t.Fatalf("Garbage = %v, want one entry", res.Garbage)
	}
}

func TestRunCheckRejectsCorruptSnapshot(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("check me please"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	key := "snapshots/7f91c6c7-7a0b-44aa-ae23-997b60e4e998.age"
	blob := backend.Get(key)
	blob[len(blob)/2] ^= 0xff
	backend.Put(key, blob)

	if _, err := runCheck(ident, backend); err == nil {
		t.Fatal("runCheck() succeeded on corrupt snapshot")
	}
}
