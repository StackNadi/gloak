# gloak standalone upload (file-2) design

Date: 2026-10-12
Status: design approved
Scope: restore the one-shot upload command as a portable folder-per-backup mode alongside the repository

## Context

Phase 1 removed the old filesystem-v1 commands (`upload`, `resume`, `cleanup`) in favor of the repository model. The repository is the right primary interface, but one-shot backups still have a clear use case: no init, one folder per backup, copy the folder anywhere and restore works. This design brings that mode back under the name `upload`, with the Phase 1 crypto envelope and without the resume/state machinery.

Repository mode and file mode are different products sharing one binary:

- Repository: one permanent layout at a remote path. Snapshots share chunks via the index. Requires `init`. Snapshot folders are not portable alone.
- File mode: each backup is a self-contained `{uuid}/` folder at the remote root. No init. No shared structure. The folder is the unit you copy or share.

Multiple repositories and many upload folders can live under the same remote root without conflict. Repositories sit at user-chosen subpaths; upload folders use random UUID names.

## Goals

- `gloak upload <file> -R <remote> [-r <recipients>]` with no prior init
- Self-contained `{uuid}/` folder: sealed chunks, encrypted manifest, public locator
- Multi-recipient manifest so any matching identity can restore
- Reuse the Phase 1 chunk envelope (zstd + XChaCha20-Poly1305)
- `restore` and `verify` auto-detect file mode versus repository mode
- Smaller and simpler than v1: no resume state, no cleanup, no per-chunk age writer

## Non-goals

- Resume, cleanup, or local upload state under `~/.gloak/uploads/`
- Deduplication or content-addressed naming inside file mode
- Reading old filesystem-v1 backups (age-writer chunks, version "1.0")
- Exporting a repository snapshot to a file folder

## Remote layout

```
{remote}/                          # user-chosen root
  personal/                        # a repository, if present
    repo.json
    ...
  {uuid}/                          # one file-mode backup
    chunk_00000                    # SealChunk output
    chunk_00001
    ...
    manifest.age                   # EncryptMulti to all recipients
    locator.json                   # public completion marker, uploaded last
```

Chunk names are sequential (`chunk_%05d`), not content-addressed. File mode does not dedup; sequential names keep the write path and the restore path trivial.

## Manifest (inside manifest.age)

JSON, compact marshal (no indent), age-encrypted with `crypto.EncryptMulti`:

| Field | Notes |
|---|---|
| `version` | `"file-2"`. Distinct from repository `2.0` and v1 `1.0`. |
| `uuid` | Folder name and backup id. |
| `original_name` | Basename of the source file. |
| `original_size` | Plaintext size in bytes. |
| `created_at` | UTC RFC3339. |
| `payload_sha256` | Hash of the full plaintext, computed while chunking. |
| `master_key` | Hex of 32 random bytes. Key for every SealChunk in this folder. |
| `chunks[]` | `core.ChunkMeta` list: index, relative name (`chunk_00000`), plain/encrypted size and SHA-256. |

The master key lives here because file mode has no `keys/` directory. One age blob holds both key material and chunk map. Restore downloads `manifest.age`, decrypts with the identity, then opens chunks.

`locator.json` stays plaintext and minimal:

```json
{"version": "file-2", "uuid": "..."}
```

Uploaded last. Restore and verify refuse a folder with no matching locator; that marks an interrupted upload as incomplete.

## Command surface

```
gloak upload <file> -R <remote> [-r <recipients>]
```

- `<file>`: kong `type:"existingfile"`.
- `-R`: remote root, required. Same rclone URI style as other commands.
- `-r`: recipient file path, comma-separated `age1...` keys, or default `~/.gloak/recipient.txt`. Reuse `resolveInitRecipients` from `init.go`.

No new top-level commands. Restore and verify keep their current flags and detect the layout.

## Upload flow

`internal/flow/upload.go`:

```
RunUpload(filePath string, recipients []string, backend storage.Backend) (uuid string, err error)
```

1. Open and stat the file. Reject empty recipient list.
2. Generate a UUID and a 32-byte master key (`crypto/rand`).
3. Read the source in `core.ChunkSize` slices with `io.ReadFull` (same loop shape as repository backup). For each non-empty slice:
   - Hash plaintext with SHA-256.
   - `crypto.SealChunk(masterKey, hash, buf[:n])` on the slice directly, no extra copy.
   - Upload to `{uuid}/chunk_%05d`.
   - Append `core.ChunkMeta`; feed the slice into the payload hasher.
4. Build the manifest struct, `json.Marshal` (compact), `crypto.EncryptMulti(recipients, ...)`, upload `{uuid}/manifest.age`.
5. Upload `{uuid}/locator.json` last.

Efficiency changes versus v1:

- Delete the entire local state file path (v1 wrote JSON after every chunk).
- SealChunk replaces the age stream writer per chunk; zstd shrinks what crosses the network.
- Pass `buf[:n]` into SealChunk instead of copying to a fresh slice.
- Recipient resolution shared with init; no second key-file parser.
- Spinner with chunk count, same style as repository backup. No byte progressbar allocation per run.

## Restore and verify detection

In `RunRestore` and `RunVerify`, before opening a repository:

1. Try `Download({uuid}/locator.json)`. If present and `uuid` matches the argument, run file-mode logic.
2. Otherwise open the repository as today.

File-mode restore:

- Download and age-decrypt `manifest.age` with the identity file.
- Require `original_name` to be a plain basename (no separators, no `..`).
- Require every chunk name to match `^chunk_\d{5}$` under the `{uuid}/` prefix. Reject anything else before downloading.
- For each chunk: download, `crypto.OpenChunk(masterKey, plainSHA256, sealed)`, check size and hash, write to the output file.
- Errors use the `CHUNK CORRUPT` prefix, same as repository restore.
- After all chunks, compare the running payload hash to `payload_sha256`.

File-mode verify:

- Same locator, manifest, and name checks.
- Download and open every chunk without writing output. Report OK or the corrupt chunk index.

## Testing

All storage tests use the memtest backend.

- Round trip: upload then restore, byte-identical output. Cover a multi-chunk file and an empty file.
- Multi-recipient: two keys at upload; restore with either identity.
- Corrupt sealed chunk: restore fails with `CHUNK CORRUPT`.
- Traversal names in the manifest (`../evil`, absolute paths): rejected before download.
- Missing locator: restore and verify refuse the folder.
- Auto-detect: a `{uuid}/` folder next to a repository; restore addresses each correctly.
- CLI: `upload` requires an existing file; short flags `-r` and `-R` resolve.

## Out of scope notes for later phases

- Reading v1 backups would need a separate tool; the age-writer format does not match SealChunk.
- If file mode ever needs resume, reusing the repository index is the wrong shape; a small per-folder state file would be the honest design.
- Backup against the same remote while another backup runs is last-writer-wins on file mode only if two UUIDs collide (negligible) or if two tools write the same folder; UUIDs prevent accidental overlap.
