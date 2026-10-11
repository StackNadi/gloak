# gloak repository model redesign

Date: 2026-10-11
Status: design approved
Scope: redesign from single-file backup to a repository model, delivered in five phases

## Positioning

gloak aims at the top tier of self-hosted backup tools (restic, borg, kopia) instead of a narrow niche. Two pillars separate it from the pack:

1. Age identity, not passphrase. A repository opens with age recipients or SSH keys. restic and kopia both depend on a remembered password.
2. No server, ever. No daemon, no rest-server, no borg serve. Every backend rclone already supports works as-is.

Every decision in this document either supports one of these pillars or closes a feature gap with the competition.

## Goals

- Directory and file backups with incremental snapshots
- Deduplication across snapshots
- restic-style retention pruning
- Authenticated per-chunk encryption with derived keys
- Multi-recipient key wrapping
- Resumable uploads as a natural consequence of the chunk index

## Non-goals

- GUI or web interface
- Server mode or multi-user access
- Compatibility with the current `filesystem-v1` single-file layout
- Deduplication performance competitive with restic on day one (correctness first, benchmarks later)

## Repository architecture

Storage layout (`filesystem-v2`):

```
{remote}/
  repo.json                 # non-secret: format version, recipient fingerprints, chunk params
  keys/
    {recipient-hash}.age    # master key wrapped per recipient
  index/
    {epoch}.age             # encrypted chunk index, one file per epoch
  snapshots/
    {snapshot-uuid}.age     # encrypted tree manifest
  data/
    ab/ab3f91...            # compressed + encrypted chunk, name = plaintext SHA-256
```

`repo.json` stays plaintext on purpose. It carries no filenames, no hashes of user data, and no key material. Its job is to let the tool recognize a gloak repository and its format version without any key, like the current `locator.json`.

Commands:

```
gloak init        # create repo layout, generate master key, wrap to recipients
gloak backup      # snapshot a path into the repo
gloak snapshots   # list snapshots
gloak restore     # restore a snapshot (with optional path filter)
gloak verify      # check one snapshot's chunks
gloak check       # check the whole repository
gloak prune       # apply a retention policy, drop unreferenced chunks
```

The current `upload`, `resume`, `verify`, and `cleanup` commands map onto `backup`, `check`, and `prune`. Deduplication absorbs the resume behavior, so the separate upload state under `~/.gloak/uploads/` goes away.

Internal package layout follows the existing `internal/` convention:

```
internal/
  repo/       # init, config, layout, master key management
  chunker/    # content-defined chunking, replaces fixed-size chunking
  index/      # encrypted chunk index plus local cache
  snapshot/   # tree manifest, directory walk
  crypto/     # existing package, extended with HKDF key derivation
  flow/       # existing per-command workflows, refactored
  storage/    # existing rclone backend
```

## Cryptography

Key hierarchy:

```
master key (32 random bytes, created by `gloak init`)
  ├── wrapped to recipient[0]  → repo/keys/{recipient0}.key  (age)
  ├── wrapped to recipient[1]  → repo/keys/{recipient1}.key  (age, multi-recipient)
  └── per-chunk key derivation:
        chunk_key = HKDF-SHA256(master, salt = plaintext_chunk_hash, info = "gloak-chunk-v1")
```

Why per-chunk keys via HKDF: each chunk gets a unique key derived from the master key and its own content hash. No per-chunk keys need to be stored or rotated, and a leaked chunk key does not lead back to the master key.

Why manifests encrypt to the recipient, not the master key: the two layers leak differently. A stolen chunk key, obtained through a bug in one chunk's handling, is useless for reading manifests and useless for other chunks, because HKDF does not run backward. A stolen manifest reveals file names and hashes but no chunk keys. Someone holding the full remote plus an identity gets everything, so this split is defense in depth for partial leaks, not protection against total compromise.

Chunk encryption uses XChaCha20-Poly1305 (`golang.org/x/crypto`, already an indirect dependency). A chunk of a few hundred kilobytes would carry a header of several hundred bytes under age, multiplied across thousands of chunks, so the smaller AEAD header wins for `data/`. Age (`filippo.io/age` v1.3.1) stays in use for `keys/`, `index/`, and `snapshots/`, where the file count is small and age's recipient model is the point.

Multi-recipient support: `recipient.txt` holds one recipient per line. `gloak init` wraps the master key once per recipient. Each identity holder unwraps the master key with their own key file.

Post-quantum support (`--pq`) uses the hybrid X25519 + ML-KEM recipients from `filippo.io/age` experimental. It ships as an opt-in flag in the final phase, not a default, because the key sizes are large and the API is not stable yet.

What the storage backend can see stays the same as today: object names built from content hashes and compressed chunk sizes. Filenames, directory structure, and plaintext hashes live only inside encrypted snapshots.

## Chunking, deduplication, compression

Content-defined chunking (CDC) replaces the fixed 20 MB split:

- Gear hash, implemented in-repo (about 50 lines, no new dependency).
- Parameters: min 512 KB, avg 1 MB, max 8 MB, configurable.
- Streaming: chunks are read and emitted without buffering the whole file.

A chunk's identity is the SHA-256 of its plaintext. Before uploading, the tool checks the index. If the hash already exists, the chunk is skipped. That single rule delivers both properties gloak needs:

- A daily backup of a mostly unchanged tree transfers only the changed chunks.
- An interrupted upload resumes for free. The index already records what landed, so the separate `~/.gloak/uploads/` state file disappears.

Compression order per chunk: read, CDC split, SHA-256, index lookup, zstd level 3, XChaCha20-Poly1305, upload to `data/{hash[:2]}/{hash}`.

zstd comes from `github.com/klauspost/compress`, the standard pure-Go implementation.

## Snapshots, restore, verify, prune

Snapshot manifest (encrypted to recipient):

```
snapshot.age =
  meta:  uuid, created_at, hostname, source paths, gloak version
  tree:  entries { rel_path, type, mode, mtime, size, chunk_hashes[] }
```

A large file is a chain of chunk hashes. A small file may fit in one chunk. Directory walk order is sorted so two backups of unchanged data produce identical trees.

Backup: walk the path tree, chunk each file, record hashes, write the snapshot manifest last. A snapshot without its manifest does not exist, which preserves the current locator-last behavior.

Restore: read and decrypt the snapshot, then per entry fetch each chunk, verify the envelope hash and the plaintext hash, decrypt, decompress, and write the output. The filename safety checks that exist today (path separators, null bytes, dot-only names, drive letters) apply to every tree entry now, not just one basename.

Verify and check:

- `gloak verify <uuid>` checks every chunk of one snapshot, both hashes.
- `gloak check` checks that every snapshot decrypts, every indexed chunk exists in storage, and every stored object is referenced or reported as garbage.

Prune:

```
gloak prune --keep-last 3 --keep-daily 7 --keep-weekly 4 --keep-monthly 6
```

Retention flags follow restic's model. Prune computes the set of live snapshots, marks every chunk they reference, deletes unreferenced objects through the storage backend, removes dead snapshot files, and compacts the index epochs into a single fresh epoch.

Prune runs as a dry run by default. Deletion requires `--yes`. A wrong retention calculation must not destroy data on the first run.

## Phasing

Phase 1: repository foundation
`gloak init` with master key generation and multi-recipient wrapping, `filesystem-v2` layout, XChaCha20-Poly1305 envelope plus zstd, epoch index, `backup` and `restore` for a single file with the existing fixed-size chunker, and a single-file `verify` so no phase ships without an integrity check. This phase puts the new cryptography and layout through a full round trip first, because key handling is the riskiest part.

Phase 2: CDC and dedup
Gear hash chunker, content-addressed `data/` objects, skip-if-exists via the index. A second identical backup uploads nothing. `verify` and `check` land here.

Phase 3: tree snapshots
Directory walk, tree manifest, `snapshots` list, multi-file restore with a path filter, per-entry filename validation.

Phase 4: prune and polish
Retention policies, index compaction, the hardcoded rclone config path fix in `internal/storage/rclone.go`, `repo.json` repository detection, CHANGELOG entry for the format break, and a first benchmark comparison against restic.

Phase 5 (optional): differentiators
SSH recipients (age already supports them), the `--pq` hybrid recipient flag, and Telegram notifications for scheduled backup reports, which was on the original roadmap.

Each phase ends with a working binary and its own tag. The repository is alpha, so `filesystem-v1` breaks in Phase 1 with a warning in the README and a CHANGELOG note. No migration tool.

## Testing

The repo already tests per package with `go test ./...`; that stays the gate, with `-race` in the existing CI.

- Unit: gear chunker boundaries and determinism, HKDF derivation stability, retention policy selection as a pure function.
- Integration against `t.TempDir()` with a fake implementation of the existing `storage.Backend` interface, so tests never shell out to rclone.
- Round trip: backup then restore must be byte-identical for random files across sizes (empty, one byte, smaller than min chunk, larger than max chunk).
- Dedup: a second backup of the same tree must issue zero uploads, counted through the fake backend.
- Prune: live snapshots keep their chunks; garbage chunks disappear; a dry run changes nothing.

## Risks

1. Scope. This is a large redesign for a solo project. The phase split keeps every step shippable; if the work pauses, a phase boundary is a usable release.
2. The format break. Covered by the existing alpha warning, plus an explicit CHANGELOG entry when Phase 1 lands.
3. A bad CDC implementation produces poor dedup instead of broken backups. Determinism tests in Phase 2 catch this before prune ever runs.
4. Prune deleting valid data. Dry-run default plus the fake-backend tests from Phase 4.

## Dependencies added

- `github.com/klauspost/compress` (zstd)
- `golang.org/x/crypto` (promoted from indirect to direct for XChaCha20-Poly1305 and HKDF)

No new external binaries beyond the rclone requirement that already exists.
