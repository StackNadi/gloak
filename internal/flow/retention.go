package flow

import (
	"fmt"
	"sort"
	"time"
)

// RetentionPolicy mirrors restic's keep rules. Zero means the rule does
// not apply.
type RetentionPolicy struct {
	KeepLast    int
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
}

func (p RetentionPolicy) isEmpty() bool {
	return p.KeepLast == 0 && p.KeepDaily == 0 && p.KeepWeekly == 0 && p.KeepMonthly == 0
}

// snapshotMeta is the subset of snapshot data retention needs.
type snapshotMeta struct {
	UUID      string
	CreatedAt time.Time
}

// selectKeepSnapshots returns the UUIDs that survive the retention policy.
// Snapshots are processed newest first. A snapshot kept by any rule stays.
// The input is mutated (sorted) in place.
func selectKeepSnapshots(snaps []snapshotMeta, p RetentionPolicy) map[string]bool {
	keep := make(map[string]bool)

	sort.Slice(snaps, func(i, j int) bool {
		if snaps[i].CreatedAt.Equal(snaps[j].CreatedAt) {
			return snaps[i].UUID < snaps[j].UUID
		}
		return snaps[i].CreatedAt.After(snaps[j].CreatedAt)
	})

	// keep-last: the N newest.
	for i := 0; i < p.KeepLast && i < len(snaps); i++ {
		keep[snaps[i].UUID] = true
	}

	// keep-daily: newest snapshot of each of the last N calendar days that
	// have at least one snapshot.
	if p.KeepDaily > 0 {
		seen := make(map[string]bool)
		count := 0
		for _, s := range snaps {
			day := s.CreatedAt.UTC().Format("2006-01-02")
			if seen[day] {
				continue
			}
			seen[day] = true
			keep[s.UUID] = true
			count++
			if count >= p.KeepDaily {
				break
			}
		}
	}

	// keep-weekly: newest snapshot of each of the last N ISO weeks.
	if p.KeepWeekly > 0 {
		seen := make(map[string]bool)
		count := 0
		for _, s := range snaps {
			year, week := s.CreatedAt.UTC().ISOWeek()
			key := fmt.Sprintf("%04d-W%02d", year, week)
			if seen[key] {
				continue
			}
			seen[key] = true
			keep[s.UUID] = true
			count++
			if count >= p.KeepWeekly {
				break
			}
		}
	}

	// keep-monthly: newest snapshot of each of the last N calendar months.
	if p.KeepMonthly > 0 {
		seen := make(map[string]bool)
		count := 0
		for _, s := range snaps {
			key := s.CreatedAt.UTC().Format("2006-01")
			if seen[key] {
				continue
			}
			seen[key] = true
			keep[s.UUID] = true
			count++
			if count >= p.KeepMonthly {
				break
			}
		}
	}

	return keep
}
