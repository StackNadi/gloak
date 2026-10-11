# gloak Phase 2: content-defined chunking and check

Date: 2026-10-12
Status: design approved
Scope: replace fixed-size repository chunking with gear-hash CDC; add structural `gloak check`

## Context

Phase 1 landed the repository layout, envelope crypto, epoch index, and single-file backup with content-addressed `data/` objects. Two pieces of Phase 2 from the parent redesign doc were already absorbed early: objects are addressed by plaintext SHA-256, and a repeat backup skips hashes already in the index. What remains is the chunker swap and the repository-wide integrity command.

## Decisions

### CDC replaces fixed-size entirely

Repository backup stops using the 20 MB fixed split. Gear hash CDC is the only chunker for `backup`. Existing Phase 1 repositories are treated as obsolete; re-run `gloak init`. No dual-mode flag, no migration.

File-mode `upload` keeps sequential fixed chunks (`chunk_00000`, `chunk_00001`, …). That path has no index and no dedup; changing it is not this phase.

### Chunk parameters live in repo.json

```go
type Config struct {
    Version    string   `json:"version"` // still "2.0"; alpha, breaking change noted in CHANGELOG
    CreatedAt  string   `json:"created_at"`
    Recipients []string `json:"recipients"`
    ChunkMin   int64    `json:"chunk_min"`
    ChunkAvg   int64    `json:"chunk_avg"`
    ChunkMax   int64    `json:"chunk_max"`
}
```

`ChunkSize` is removed. Defaults at init: min 512 KiB, avg 1 MiB, max 8 MiB. Optional init flags `--chunk-min`, `--chunk-avg`, `--chunk-max` override them. `repo.Open` rejects a config where any value is non-positive or `max < avg < min` fails the ordering (`max >= avg >= min`).

### check is structural

`gloak check` does not download chunk payloads. It proves the graph is consistent without paying for a full re-encrypt pass. `gloak verify <snapshot>` remains the deep per-chunk check.

## Chunker package

New package `internal/chunker`:

- 256-entry gear table, fixed at package init from a hardcoded seed. Same binary, same table, always.
- `NewCDC(min, avg, max int64) (*CDC, error)`. Validate positivity and `max >= avg >= min`. Snap `max` up to a multiple of 64 (gear window size).
- Streaming split over `io.Reader` via callback: `func (c *CDC) Split(r io.Reader, fn func(chunk []byte) error) error`. One chunk buffer is allocated per emit; the caller may retain it.
- Boundary rule: fold 64-byte gear window; cut when `gear % avg == 0` and `size >= min`, or when `size >= max`. Force a cut at EOF. A reader shorter than `min` yields one chunk.
- Chunk identity stays SHA-256 of the plaintext. Envelope crypto, index entries, and `data/{hh}/{hash}` paths are unchanged.

The existing `core.StreamChunker*` fixed-size helpers remain for file-mode upload only.

## backup changes

`runBackup` opens the CDC from `r.Config.ChunkMin/Avg/Max` and splits the file through it. Per chunk: hash, index lookup, seal, upload if missing, record meta. Checkpoint the epoch index every N uploads as today. Snapshot manifest is unchanged in shape; chunk `Name` is still the `data/…` path.

A second backup of identical content issues zero uploads. A backup of a file with a small insertion reuses every chunk whose bytes did not move across a boundary.

## check command

`CheckCmd` (`gloak check -R <remote> -r <identity>`):

1. `repo.Open`, then `index.Load`.
2. List `snapshots/`. For each entry, download and decrypt the manifest with the identity. Any decrypt failure is an error.
3. Build the referenced set: every plaintext hash from every snapshot, union every index entry hash.
4. List `data/` recursively.
5. Classify:
   - referenced but object absent → missing (error, exit non-zero)
   - object present but unreferenced → garbage (printed as a warning count, not an error)
   - object present and referenced → ok

Missing means a later restore will fail; check surfaces that before a disaster. Garbage means prune (Phase 4) has work; check only reports it.

## Testing

- Chunker unit tests: determinism (same input, same boundaries across two `CDC` values), cuts near min and max, one-byte file, file smaller than min, file exactly max, multi-MB random file splits into more than one chunk.
- Dedup: backup the same file twice against `memtest`; second run has `UploadedChunks == 0`.
- CDC quality: insert one byte near the start of a multi-max-size file; most later chunk hashes still match the pre-insert backup.
- check: clean repo passes; delete a `data/` object → missing; plant an unreferenced object → garbage warning only; corrupt a snapshot → decrypt error.
- Round trip: backup then restore of random files across sizes stays byte-identical.

## Out of scope

- Prune, retention, index compaction (Phase 4).
- Directory trees and multi-file snapshots (Phase 3).
- Changing file-mode upload chunking.
- Migration tool for Phase 1 fixed-size repositories.

## Parent design

Phase 2 of `docs/superpowers/specs/2026-10-11-gloak-repo-model-design.md`. This spec replaces the "CDC and dedup" section where they overlap; dedup via index is already done.
