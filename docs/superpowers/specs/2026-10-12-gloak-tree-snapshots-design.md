# gloak Phase 3: tree snapshots

Date: 2026-10-12
Status: design approved by owner in advance (pre-approved for overnight execution)
Scope: directory backups, tree manifests, snapshots list, multi-file restore with path filter, per-entry path validation

## Context

Phase 2 left repository backup as a single-file pipeline: one source path, one flat chunk list, one output file. Phase 3 of the parent redesign adds directory trees without changing crypto, CDC, index, or storage layout.

## Decisions

### Snapshot kinds

The on-disk path stays `snapshots/{uuid}.age`. The decrypted JSON gains a `kind` field:

- `"file"`: the existing single-file `core.Manifest`. Field set unchanged.
- `"tree"`: the new directory manifest below.

Missing `kind` is treated as `"file"` so any Phase 2 snapshot still opens. Format version string stays `"2.0"`.

### Tree manifest

```go
type TreeManifest struct {
    Version   string      `json:"version"` // repo.FormatVersion
    UUID      string      `json:"uuid"`
    Kind      string      `json:"kind"` // always "tree"
    CreatedAt string      `json:"created_at"`
    Hostname  string      `json:"hostname"`
    Paths     []string    `json:"paths"` // absolute source paths as given
    Entries   []TreeEntry `json:"entries"`
}

type TreeEntry struct {
    RelPath string      `json:"rel_path"` // slash-separated, relative to the source root
    Type    string      `json:"type"`     // "file" or "dir"
    Mode    uint32      `json:"mode"`
    Mtime   string      `json:"mtime"`    // RFC3339 UTC
    Size    int64       `json:"size"`     // files only; dirs 0
    Chunks  []ChunkMeta `json:"chunks"`   // files only; dirs empty
}
```

Walk order: lexical by `RelPath` (slash form). Dirs are recorded so restore recreates empty directories. Symlinks and other non-regular, non-dir types are skipped with a warning count, not an error.

### Backup path handling

`backup <path>` accepts a file or a directory:

- File: current single-file flow, `kind` file. Unchanged behavior.
- Directory: walk tree, CDC each regular file through the shared index/seal path, write one tree snapshot last. Source root basename becomes the restore folder name; `RelPath` is relative to that root.

One snapshot id per command run, same as today. Empty directory produces a tree with dir entries only and zero chunks.

### Restore

`restore <uuid> -o <dir>`:

- File snapshot: unchanged.
- Tree snapshot: recreate directories and files under `<dir>/<basename-of-source>/`. Optional `--path <rel>` filter restores only entries whose `RelPath` equals the filter or sits under it. Empty filter restores everything.

Output modes: dirs 0755 default, files use entry mode masked with 0777. Mtime set with `os.Chtimes` after write.

### verify

File snapshot: unchanged deep check. Tree snapshot: download every chunk of every file entry with the same dual-hash and envelope rules, then stop. No directory attribute check.

### check

When walking snapshots, accept both kinds. Tree chunks feed the same referenced set as file chunks. Corrupt or undecryptable snapshot is still an error.

### snapshots list

`gloak snapshots -R <remote> [-i identity]` lists every snapshot:

```
UUID  KIND  CREATED  HOST  DETAIL
```

`DETAIL` is original name for file kind, file count and top-level source basename for tree kind. Sorted by created time descending. No snapshot: print "No snapshots found." and exit 0.

### Path validation (tree entries)

`validateRelPath(rel string) error` rejects:

- empty path
- absolute path (leading `/` or Windows drive letter `C:`)
- any segment that is empty, `.`, or `..`
- null byte anywhere
- backslash anywhere (force slash form)

Applied on restore for every entry. Backup already builds safe relative paths from `filepath.Rel` and slash conversion; restore is the trust boundary because the manifest is remote input.

### CLI surface

- `BackupCmd.ExistingFile` type changes from `existingfile` to a plain string path so directories are accepted. Missing path is still an error from the flow layer.
- `RestoreCmd` gains `--path` filter (`PathFilter string`).
- New `SnapshotsCmd` with `-R` required, `-i` optional identity.

### Out of scope

- Hard links, devices, sockets, extended attributes, ACLs.
- Incremental directory walk across snapshots (Phase 4 prune still only uses chunk index).
- Changing file-mode `upload`.
- Migration tools.

## Testing

- Tree backup of temp dir with nested files and one empty dir: snapshot decrypts, entries sorted, empty dir present, file bytes match.
- Symlink in tree: skipped, backup still succeeds.
- Restore tree with filter: only subtree written; full restore byte-identical.
- Restore rejects `../escape` and absolute RelPath in crafted manifest.
- verify tree: clean pass, corrupt chunk fails with CHUNK CORRUPT.
- check with mixed file and tree snapshots: referenced set unions both.
- snapshots list: two snapshots, both rows present.
- Single-file backup/restore/verify regression green.
