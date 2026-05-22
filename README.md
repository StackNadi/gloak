# gloak

> Go single-binary CLI for age-encrypted, chunked, verified backups.

Encrypt files, split into chunks with SHA-256 checks, upload, verify, restore. Metadata (filenames, sizes, recipients, hashes) lives inside an age-encrypted manifest. Storage never sees plaintext metadata.

```text
keygen → encrypt → chunk → encrypt manifest → upload → verify → restore
```

---

## Quick start

```bash
# install dependencies
go mod download

# build standalone binary
go build -o gloak .

# generate keys
./gloak keygen

# encrypt + upload
./gloak upload ./secret.tar --remote myremote:backup_folder

# verify
./gloak verify <backup-id> --remote myremote:backup_folder -i ~/.gloak/identity.txt

# restore
./gloak restore <backup-id> --remote myremote:backup_folder --output-dir ./restored/
```

---

## Why this exists

Most backup tools either:

1. Encrypt a single blob — good luck retrying after a network failure at byte 47 GB of 50.
2. Split into chunks without integrity metadata — you find out a chunk is corrupt during restore, with no manifest to guide repair.
3. Store metadata in plaintext — filenames, sizes, recipient keys, and chunk hashes visible to anyone who reads the storage.
4. Pull in rclone, restic rest-server, or S3 libraries when you just need local filesystem or a mounted remote.

gloak is the boring correct version: age encryption, fixed-size chunks, deterministic manifests, integrity checks on every chunk *and* the full payload, and an encrypted manifest envelope so storage never sees filenames or hashes.

---

## Install

```bash
go mod download
```

Build the standalone binary:

```bash
go build -o gloak .
```

Produces `gloak` — a compiled binary. Drop it into containers, cron jobs, or VPS rootfs without needing Node, Bun, or Python installed.

Run tests:

```bash
go test ./...
```

---

## Key generation

Generate a keypair once:

```bash
./gloak keygen
```

Default location is `~/.gloak/`:

```text
~/.gloak/
  identity.txt      # private key — keep this secret
  recipient.txt     # public key — safe to share
```

Use a custom directory:

```bash
./gloak keygen --output-dir /path/to/keys
./gloak keygen -o /path/to/keys
```

After `keygen` runs once, `-i` (identity) and `-r` (recipient) flags become optional. gloak reads from `~/.gloak/` automatically.

### Key flags

| Flag | Alias | Description |
|------|-------|-------------|
| `-r <file>` | `--recipient` | Recipient public key file. Overrides `~/.gloak/recipient.txt`. |
| `-i <file>` | `--identity` | Identity private key file. Overrides `~/.gloak/identity.txt`. |

---

## Commands

### `setup` — install dependencies

If you don't have `rclone` installed on your system, `gloak setup` will download a standalone, verified binary to `~/.gloak/bin/rclone` so `gloak` can use it without touching your system packages.

```bash
./gloak setup
```

| Option | Alias | Description |
|--------|-------|-------------|
| `--dir <path>` | `-D` | Custom install directory (default: `~/.gloak/bin`). |
| `--update` | `-u` | Force re-download even if `rclone` already exists. |

---

### `upload` — full encrypted backup flow

Creates a UUID folder, encrypts the input, splits into chunks, writes SHA-256 checksums into an age-encrypted manifest, and uploads in order: chunks first, manifest second, locator last.

Chunks and manifest are encrypted with the same age recipient. Storage sees filenames like `chunk_00000` and `manifest.age` but cannot read filenames, sizes, or hashes without the identity key.

```bash
# using auto-resolved keys
./gloak upload ./backup.tar --remote myremote:backup_folder

# with explicit recipient
./gloak upload ./backup.tar \
  -r /path/to/recipient.txt \
  --remote myremote:backup_folder
```

Output:

```text
Backup ID: 7f91c6c7-7a0b-44aa-ae23-997b60e4e998
Backup completed successfully!
Save this UUID for restoration: 7f91c6c7-7a0b-44aa-ae23-997b60e4e998
```

| Option | Default | Description |
|--------|---------|-------------|
| `--remote` | _required_ | Target Rclone destination (e.g. `myremote:backup_folder` or `/mnt/backups`). |

The locator is uploaded last. If the upload is interrupted, the backup directory exists but has no `locator.json`. Upload refuses to overwrite on retry, so partial uploads never look complete.

---

### `verify` — verify backup integrity

Downloads and decrypts `manifest.age`, checks every chunk exists with matching size and SHA-256, then checks the concatenated encrypted payload hash against the manifest.

```bash
./gloak verify 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --remote myremote:backup_folder \
  -i ~/.gloak/identity.txt
```

Output on success:

```text
VERIFIED! All chunks are healthy and checksums match 100%.
```

On failure:

```text
CHUNK CORRUPT: Hash chunk_00042 mismatch!
```

| Option | Description |
|--------|-------------|
| `-i <file>` | Identity private key to decrypt `manifest.age` (defaults to `~/.gloak/identity.txt`). |

---

### `restore` — restore a backup

Verifies the full backup first (every chunk, every hash), then concatenates chunks and decrypts the payload.

```bash
# with auto-resolved identity
./gloak restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --remote myremote:backup_folder \
  --output-dir ./restored/

# with explicit identity
./gloak restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --remote myremote:backup_folder \
  -i ~/.gloak/identity.txt \
  --output-dir ./restored/
```

If `--output-dir` is a directory (trailing `/` or existing directory), gloak restores the original filename from the decrypted manifest after validating it is safe. If `--output-dir` is a file path, it writes there directly.

Restore rejects:

- Backup IDs that fail UUID v4 validation
- Corrupted or tampered `manifest.age`
- Unsafe filenames in the decrypted manifest (path separators, null bytes, dots only, Windows drive letters)
- Chunks with wrong size or SHA-256

---

### `cleanup` — remove incomplete backups

Scans the remote storage for backup directories that are missing `manifest.age` (usually caused by interrupted uploads) and offers to delete them.

```bash
# interactive prompt before deletion
./gloak cleanup --remote myremote:backup_folder

# skip prompt (useful for cron jobs)
./gloak cleanup --remote myremote:backup_folder --yes
```

| Option | Alias | Description |
|--------|-------|-------------|
| `--yes` | `-y` | Skip confirmation prompt. |

---

## Storage layout

### v2 (current, default)

```text
/mnt/backups/
  {backup_id}/
    locator.json          # public completion marker
    manifest.age          # age-encrypted manifest
    chunk_00000           # encrypted chunk (fixed-size)
    chunk_00001
    chunk_00002
```

**`locator.json`** is a small public file:

```json
{
  "version": 2,
  "app": "gloak",
  "backup_id": "7f91c6c7-7a0b-44aa-ae23-997b60e4e998",
  "manifest": { "name": "manifest.age", "encryption": "age" },
  "storage": { "layout": "filesystem-v2" }
}
```

The locator has no sensitive metadata. No filename, no recipient, no chunk hashes, no payload hash. It exists so tools can detect backup completion without decrypting anything.

**`manifest.age`** is the full backup metadata encrypted with age. Once decrypted:

| Field | Contains |
|-------|----------|
| `source.original_filename` | Basename of backed-up file |
| `source.original_size` | Original file size |
| `payload.encryption.recipients` | Age recipient(s) |
| `payload.encrypted_file_sha256` | SHA-256 of encrypted payload |
| `chunking.chunk_size` | Fixed chunk size |
| `chunking.total_chunks` | Number of chunks |
| `chunks[].index` | Chunk index (0-based) |
| `chunks[].name` | Chunk filename (e.g. `000042.chunk`) |
| `chunks[].size` | Chunk byte size |
| `chunks[].sha256` | SHA-256 of chunk content |

### v1 (legacy)

Older backups may have `manifest.json` instead of `manifest.age`. Plaintext JSON with the same metadata format but version `1`.

---

## Architecture

```text
├── main.go                     # CLI entrypoint
├── setup.go                    # rclone downloader command
├── keygen.go                   # keygen command definition
├── upload.go                   # upload command definition
├── verify.go                   # verify command definition
├── restore.go                  # restore command definition
├── cleanup.go                  # cleanup command definition
└── internal/
    ├── core/
    │   ├── chunk.go            # fixed-size chunking + hashing
    │   └── manifest.go         # Manifest types + validation
    ├── crypto/
    │   ├── keygen.go           # age keypair generation
    │   └── stream.go           # streaming age encrypt/decrypt wrapper
    ├── flow/
    │   ├── upload.go           # upload workflow logic
    │   ├── verify.go           # verification workflow logic
    │   └── restore.go          # restore workflow logic
    └── storage/
        └── rclone.go           # Rclone backend wrapper
```

### Data flow

```text
upload:
  input file → age encrypt → encrypted payload
                            → split into fixed-size chunks
                            → SHA-256 each chunk
                            → manifest.age (with all metadata)
                            → upload chunks → upload manifest.age → upload locator.json

verify:
  locator.json → validate structure
              → manifest.age → age decrypt → validate ManifestV2
                                           → check every chunk exists
                                           → check every chunk size
                                           → check every chunk SHA-256
                                           → concatenate chunks
                                           → check encrypted payload SHA-256

restore:
  verify (full) → download chunks → concatenate → decrypt payload → write output
```

### Key design decisions

| Decision | Rationale |
|----------|-----------|
| Recipient-mode age encryption | One identity decrypts. No passphrase to remember or leak. |
| Fixed-size chunks | Deterministic boundaries regardless of content. Simplifies verification, resumability, and storage layout. |
| Encrypted manifest | Storage never sees filenames, sizes, or hashes. Only backup UUIDs and chunk object names are visible. |
| Locator uploaded last | Atomicity marker. If upload is interrupted, no `locator.json` — upload refuses to overwrite on retry. |
| Identity required for verify/restore | Manifest is encrypted. You need the identity key to read it. This is a feature, not a bug. |
| SHA-256 at chunk + payload | Repair individual chunks without re-downloading everything. Corruption detected before decrypt. |
| Fixed 20MB chunk size | Prevents metadata bloat from tiny chunks and excessive memory usage. |

---

## Security model

### What is protected

| Threat | Mitigation |
|--------|------------|
| Storage reads manifest metadata | Manifest encrypted with age. Filenames, sizes, recipient keys, and chunk hashes are ciphertext. |
| Storage tampers with manifest | Decryption fails or decrypted manifest fails validation: backup_id mismatch, unsafe filename, invalid chunk hashes, wrong chunk count, non-contiguous indexes. |
| Path traversal in filename | Go path sanitization rejects path separators, null bytes, dots-only names, Windows drive letters. Directory traversal checks ensure the output stays under the chosen restore path. |
| Storage replaces a chunk | SHA-256 verification catches size or content mismatch at chunk and payload level. |
| Storage replays an old locator | Locator backup_id must match the requested backup ID. Manifest backup_id must also match after decrypt. |
| Unauthorized upload | Upload requires an age recipient public key. Anyone who can read the recipient file can encrypt. (The recipient file is a public key — security depends on access control to it.) |
| Duplicate backup overwrite | Upload refuses if `locator.json` or `manifest.age` already exists. |

### What is NOT protected

| Limitation | Explanation |
|------------|-------------|
| No writer provenance | Age encryption proves the encryptor knows the recipient, not who they are. A malicious storage provider with access to the recipient file could upload a fake backup that decrypts. |
| Metadata side channels | Backup UUIDs visible in storage listings. Chunk count and sizes visible from object metadata. File size inferred from chunk count × chunk size (minus padding). |
| In-transit encryption | Transport security is the storage backend's job. Local filesystem has none; S3/R2 would use HTTPS. |
| Streaming memory | Encrypt/decrypt uses Go `io.Pipe`, `io.MultiWriter`, and `filippo.io/age` streams chunks directly to `rclone` standard input. Zero disk footprint for chunks, stable RAM usage under 50MB regardless of file size. |

### Vulnerability audit

See [`VULNERABILITIES.md`](./VULNERABILITIES.md) for the full audit of 10 findings (SB-VULN-001 through SB-VULN-010), all remediated.

Current status:

```text
Audit:   govulncheck ./... — 0 known vulnerabilities
Build:   Go 1.22+ standalone binary
```

---

## Design spec

[`MANIFEST_AUTH_SPEC.md`](./MANIFEST_AUTH_SPEC.md) documents the encrypted manifest v2 design:

- Goals and non-goals
- Threat model
- Storage layout
- Validation rules (60+ invariant checks)
- Acceptance criteria

---

## Not built yet

Deferred intentionally:

| Feature | Reasoning |
|---------|-----------|
| Named storage profiles | `--remote my-backups` instead of typing URIs. |
| Resumable uploads | Track uploaded chunks, skip on retry. Needs state file in backup directory. |
| Telegram integration | Hermes Agent plugin for backup/restore/verify via Telegram. |
| Per-chunk encryption keys | Each chunk encrypted with a unique key, stored in manifest. Limits blast radius of key exposure. |

---

## License

MIT
