package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/pterm/pterm"

	"gloak/internal/flow"
)

type PruneCmd struct {
	Identity    string `help:"Identity private key file (defaults to ~/.gloak/identity.txt)." short:"i"`
	Remote      string `help:"Source Rclone URI (e.g., keraaaki:backup_folder or /mnt/backups)." required:"" short:"R"`
	KeepLast    int    `help:"Keep the N newest snapshots." default:"0"`
	KeepDaily   int    `help:"Keep the newest snapshot of each of the last N days." default:"0"`
	KeepWeekly  int    `help:"Keep the newest snapshot of each of the last N weeks." default:"0"`
	KeepMonthly int    `help:"Keep the newest snapshot of each of the last N months." default:"0"`
	Yes         bool   `help:"Actually delete. Without this flag prune is a dry run."`
}

// Run prints a retention plan, applying deletions only when Yes is set.
// It returns prune errors and sends an optional notification after a successful apply.
func (c *PruneCmd) Run() error {
	pterm.Info.Println("Starting PRUNE process...")
	ident := c.Identity
	if ident == "" {
		ident = filepath.Join(getDefaultDir(), "identity.txt")
		pterm.Info.Printf("Using identity from: %s\n", ident)
	}
	policy := flow.RetentionPolicy{
		KeepLast:    c.KeepLast,
		KeepDaily:   c.KeepDaily,
		KeepWeekly:  c.KeepWeekly,
		KeepMonthly: c.KeepMonthly,
	}
	start := time.Now()
	res, err := flow.RunPrune(ident, c.Remote, policy, c.Yes)
	if err != nil {
		return err
	}
	pterm.Printf("Kept snapshots: %d\n", len(res.KeptSnapshots))
	pterm.Printf("Dead snapshots: %d\n", len(res.DeadSnapshots))
	pterm.Printf("Live chunks: %d\n", res.LiveChunks)
	pterm.Printf("Garbage chunks: %d\n", res.GarbageChunks)
	if !c.Yes {
		pterm.Warning.Println("Dry run only. Pass --yes to delete.")
		return nil
	}
	pterm.Success.Printf("Deleted %d snapshots and %d objects; compacted index: %v\n",
		res.DeletedSnapshots, res.DeletedObjects, res.CompactedEpochs)
	notifyCommand("prune",
		fmt.Sprintf("Kept: %d snapshots", len(res.KeptSnapshots)),
		fmt.Sprintf("Dead: %d snapshots", len(res.DeadSnapshots)),
		fmt.Sprintf("Live chunks: %d", res.LiveChunks),
		fmt.Sprintf("Garbage chunks: %d", res.GarbageChunks),
		fmt.Sprintf("Deleted: %d snapshots, %d objects", res.DeletedSnapshots, res.DeletedObjects),
		"Duration: "+formatDuration(time.Since(start)),
	)
	return nil
}
