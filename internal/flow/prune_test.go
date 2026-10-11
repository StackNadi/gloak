package flow

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSelectKeepSnapshotsLastOnly(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	snaps := []snapshotMeta{
		{UUID: "a", CreatedAt: base.Add(-72 * time.Hour)},
		{UUID: "b", CreatedAt: base.Add(-48 * time.Hour)},
		{UUID: "c", CreatedAt: base.Add(-24 * time.Hour)},
		{UUID: "d", CreatedAt: base},
	}
	keep := selectKeepSnapshots(snaps, RetentionPolicy{KeepLast: 2})
	if len(keep) != 2 || !keep["d"] || !keep["c"] {
		t.Fatalf("keep = %v", keep)
	}
}

func TestSelectKeepSnapshotsDaily(t *testing.T) {
	// Three snapshots on same day, one older day, one older still.
	day := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	snaps := []snapshotMeta{
		{UUID: "old", CreatedAt: day.Add(-48 * time.Hour)},
		{UUID: "mid", CreatedAt: day.Add(-24 * time.Hour)},
		{UUID: "m1", CreatedAt: day},
		{UUID: "m2", CreatedAt: day.Add(2 * time.Hour)},
	}
	keep := selectKeepSnapshots(snaps, RetentionPolicy{KeepDaily: 2})
	// Newest of day1 (m2) and newest of day0 (mid).
	if !keep["m2"] || !keep["mid"] {
		t.Fatalf("keep = %v", keep)
	}
	if keep["m1"] || keep["old"] {
		t.Fatalf("unexpected keep = %v", keep)
	}
}

func TestSelectKeepSnapshotsEmptyPolicyKeepsNothing(t *testing.T) {
	snaps := []snapshotMeta{{UUID: "a", CreatedAt: time.Now()}}
	keep := selectKeepSnapshots(snaps, RetentionPolicy{})
	if len(keep) != 0 {
		t.Fatalf("keep = %v", keep)
	}
}

func TestRunPruneRejectsEmptyPolicy(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	_, err := runPrune(ident, backend, RetentionPolicy{}, false)
	if err == nil {
		t.Fatal("runPrune() succeeded with empty policy")
	}
}

func TestRunPruneDryRunChangesNothing(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	filePath := writeTempFile(t, []byte("keep-me"))
	if _, err := runBackup(filePath, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	before := backend.TotalUploads("")
	snapBefore := backend.Has("snapshots/7f91c6c7-7a0b-44aa-ae23-997b60e4e998.age")

	res, err := runPrune(ident, backend, RetentionPolicy{KeepLast: 1}, false)
	if err != nil {
		t.Fatalf("runPrune() error: %v", err)
	}
	if res.DeletedSnapshots != 0 || res.DeletedObjects != 0 {
		t.Fatalf("dry-run deleted something: %+v", res)
	}
	if len(res.KeptSnapshots) != 1 || len(res.DeadSnapshots) != 0 {
		t.Fatalf("plan = %+v", res)
	}
	if backend.TotalUploads("") != before || backend.Has("snapshots/7f91c6c7-7a0b-44aa-ae23-997b60e4e998.age") != snapBefore {
		t.Fatal("dry-run mutated backend")
	}
}

func TestRunPruneYesDeletesDead(t *testing.T) {
	ident, backend, _ := repoFixture(t)
	// Two snapshots with different content so second has unique chunks.
	f1 := filepath.Join(t.TempDir(), "one.txt")
	if err := os.WriteFile(f1, []byte("content-one-unique"), 0644); err != nil {
		t.Fatal(err)
	}
	f2 := filepath.Join(t.TempDir(), "two.txt")
	if err := os.WriteFile(f2, []byte("content-two-different"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runBackup(f1, ident, backend, "7f91c6c7-7a0b-44aa-ae23-997b60e4e998", nil); err != nil {
		t.Fatal(err)
	}
	// Distinct RFC3339 timestamps so keep-last is deterministic.
	time.Sleep(1100 * time.Millisecond)
	if _, err := runBackup(f2, ident, backend, "8f91c6c7-7a0b-44aa-ae23-997b60e4e999", nil); err != nil {
		t.Fatal(err)
	}

	res, err := runPrune(ident, backend, RetentionPolicy{KeepLast: 1}, true)
	if err != nil {
		t.Fatalf("runPrune() error: %v", err)
	}
	if res.DeletedSnapshots != 1 {
		t.Fatalf("DeletedSnapshots = %d, want 1", res.DeletedSnapshots)
	}
	if backend.Has("snapshots/7f91c6c7-7a0b-44aa-ae23-997b60e4e998.age") {
		t.Fatal("dead snapshot still present")
	}
	if !backend.Has("snapshots/8f91c6c7-7a0b-44aa-ae23-997b60e4e999.age") {
		t.Fatal("kept snapshot deleted")
	}
	// Live snapshot must still verify.
	if err := runVerify("8f91c6c7-7a0b-44aa-ae23-997b60e4e999", ident, backend); err != nil {
		t.Fatalf("verify after prune: %v", err)
	}
}
