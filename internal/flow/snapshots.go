package flow

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/pterm/pterm"

	"gloak/internal/core"
	"gloak/internal/repo"
	"gloak/internal/storage"
)

type SnapshotInfo struct {
	UUID      string
	Kind      string
	CreatedAt string
	Hostname  string
	Detail    string
}

func RunSnapshots(identityPath, remote string) error {
	backend := storage.NewRcloneBackend(remote)
	infos, err := runSnapshots(identityPath, backend)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		pterm.Info.Println("No snapshots found.")
		return nil
	}
	table := pterm.DefaultTable.
		WithHasHeader().
		WithData(append([][]string{{"UUID", "KIND", "CREATED", "HOST", "DETAIL"}}, snapshotRows(infos)...))
	_ = table.Render()
	return nil
}

func snapshotRows(infos []SnapshotInfo) [][]string {
	rows := make([][]string, 0, len(infos))
	for _, info := range infos {
		rows = append(rows, []string{info.UUID, info.Kind, info.CreatedAt, info.Hostname, info.Detail})
	}
	return rows
}

func runSnapshots(identityPath string, backend storage.Backend) ([]SnapshotInfo, error) {
	if _, err := repo.Open(backend, identityPath); err != nil {
		return nil, err
	}
	raw, err := backend.ListFiles([]string{repo.SnapshotsPrefix})
	if err != nil {
		// Fresh repo with no snapshots directory.
		return nil, nil
	}
	entries, err := storage.ParseList(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse snapshot listing: %w", err)
	}
	var infos []SnapshotInfo
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Path, ".age") {
			continue
		}
		id := strings.TrimSuffix(path.Base(entry.Path), ".age")
		kind, fileM, treeM, err := readAnyManifest(backend, identityPath, id)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", id, err)
		}
		info := SnapshotInfo{UUID: id, Kind: kind}
		if kind == core.KindTree {
			info.CreatedAt = treeM.CreatedAt
			info.Hostname = treeM.Hostname
			detail := fmt.Sprintf("%d files", treeM.FileCount())
			if len(treeM.Paths) > 0 {
				detail += " from " + path.Base(strings.TrimRight(treeM.Paths[0], "/"))
			}
			info.Detail = detail
		} else {
			info.CreatedAt = fileM.CreatedAt
			info.Detail = fileM.OriginalName
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].CreatedAt == infos[j].CreatedAt {
			return infos[i].UUID < infos[j].UUID
		}
		return infos[i].CreatedAt > infos[j].CreatedAt
	})
	return infos, nil
}
