# SecureBackup

> Bun single-binary CLI for age-encrypted, chunked, verified backups.

Encrypt files, split into chunks with SHA-256 checks, upload, verify, restore. Metadata (filenames, sizes, recipients, hashes) lives inside an age-encrypted manifest. Storage never sees plaintext metadata.

```text
keygen → encrypt → chunk → encrypt manifest → upload → verify → restore
```

---

## Quick start

```bash
# install dependencies
bun install

# build standalone binary
bun build src/cli.ts --compile --outfile dist/securebackup

# generate keys
./dist/securebackup keygen

# encrypt + upload
./dist/securebackup upload ./secret.tar --to local:/mnt/backups

# verify
./dist/securebackup verify <backup-id> --from local:/mnt/backups -i ~/.securebackup/identity.txt

# restore
./dist/securebackup restore <backup-id> --from local:/mnt/backups --output ./restored/
```

---

## Why this exists

Most backup tools either:

1. Encrypt a single blob — good luck retrying after a network failure at byte 47 GB of 50.
2. Split into chunks without integrity metadata — you find out a chunk is corrupt during restore, with no manifest to guide repair.
3. Store metadata in plaintext — filenames, sizes, recipient keys, and chunk hashes visible to anyone who reads the storage.
4. Pull in rclone, restic rest-server, or S3 libraries when you just need local filesystem or a mounted remote.

SecureBackup is the boring correct version: age encryption, fixed-size chunks, deterministic manifests, integrity checks on every chunk *and* the full payload, and an encrypted manifest envelope so storage never sees filenames or hashes.

---

## Install

```bash
bun install
```

Build the standalone binary:

```bash
bun run build
```

Produces `dist/securebackup` — single binary, no runtime dependencies. Drop it into containers, cron jobs, or VPS rootfs without Node or Bun installed.

Run tests:

```bash
bun test
```

Dependency audit:

```bash
bun audit
```

---

## Key generation

Generate a keypair once:

```bash
./dist/securebackup keygen
```

Default location is `~/.securebackup/`:

```text
~/.securebackup/
  identity.txt      # private key — keep this secret
  recipient.txt     # public key — safe to share
```

Use a custom directory:

```bash
./dist/securebackup keygen --output /path/to/keys
./dist/securebackup keygen -o /path/to/keys
```

After `keygen` runs once, `-i` (identity) and `-r` (recipient) flags become optional. SecureBackup reads from `~/.securebackup/` automatically.

### Key flags

| Flag | Alias | Description |
|------|-------|-------------|
| `-r <file>` | `--recipient` | Recipient public key file. Overrides `~/.securebackup/recipient.txt`. |
| `-i <file>` | `--identity` | Identity private key file. Overrides `~/.securebackup/identity.txt`. |

---

## Commands

### `encrypt` — encrypt one file or split into chunks

Two modes.

**Single encrypted file:**

```bash
./dist/securebackup encrypt ./secret.txt --output ./secret.txt.age
```

**Split into encrypted chunks:**

```bash
./dist/securebackup encrypt ./video.tar \
  --out-dir ./video-encrypted-chunks \
  --chunk-size 20MB
```

Output:

```text
video-encrypted-chunks/
  000000.chunk
  000001.chunk
  000002.chunk
```

| Option | Description |
|--------|-------------|
| `--output <file>` | Write single encrypted `.age` file. |
| `--out-dir <dir>` | Split encrypted output into fixed-size chunks. |
| `--chunk-size <size>` | Chunk size. Supports `64KiB` to `1GiB`. Default: `20MB`. |

Chunks from `encrypt --out-dir` have **no manifest**. They are bare encrypted chunks. Use `upload` if you need backup metadata and verification.

> [!WARNING]
> Never `cat` encrypted `.age` files or chunk files. They are binary and your terminal will look like an Etch-a-Sketch after a seizure.

---

### `decrypt` — decrypt a file or chunk directory

**Single file:**

```bash
./dist/securebackup decrypt ./secret.txt.age --output ./secret.txt
```

**Chunk directory (requires a manifest for integrity verification):**

```bash
./dist/securebackup decrypt \
  --chunks-dir ./video-encrypted-chunks \
  --manifest ./manifest.json \
  -i ~/.securebackup/identity.txt \
  --output ./video.tar
```

| Option | Description |
|--------|-------------|
| `--chunks-dir <dir>` | Directory containing encrypted chunks. |
| `--manifest <file>` | Manifest JSON file (required with `--chunks-dir`). |

Direct chunk decryption requires a manifest because decrypting corrupt chunks silently produces garbage. The manifest lets you verify every chunk's SHA-256 before touching the `age` ciphertext.

> [!NOTE]
> For normal backup workflows, use `restore`. It handles manifest decryption, integrity checks, and identity resolution automatically.

---

### `upload` — full encrypted backup flow

Creates a UUID folder, encrypts the input, splits into chunks, writes SHA-256 checksums into an age-encrypted manifest, and uploads in order: chunks first, manifest second, locator last.

Chunks and manifest are encrypted with the same age recipient. Storage sees filenames like `000000.chunk` and `manifest.age` but cannot read filenames, sizes, or hashes without the identity key.

```bash
# using auto-resolved keys
./dist/securebackup upload ./backup.tar --to local:/mnt/backups

# with explicit recipient
./dist/securebackup upload ./backup.tar \
  -r /path/to/recipient.txt \
  --to local:/mnt/backups
```

Output:

```text
Backup uploaded successfully.

Backup ID:
7f91c6c7-7a0b-44aa-ae23-997b60e4e998

Chunks:
205

Remote:
local:/mnt/backups/7f91c6c7-7a0b-44aa-ae23-997b60e4e998
```

| Option | Default | Description |
|--------|---------|-------------|
| `--chunk-size` | `20MB` | Chunk size (64KiB min, 1GiB max). |
| `--to <uri>` | _required_ | Storage destination, e.g. `local:/mnt/backups`. |

The locator is uploaded last. If the upload is interrupted, the backup directory exists but has no `locator.json`. Upload refuses to overwrite on retry, so partial uploads never look complete.

---

### `verify` — verify backup integrity

Downloads and decrypts `manifest.age`, checks every chunk exists with matching size and SHA-256, then checks the concatenated encrypted payload hash against the manifest.

```bash
./dist/securebackup verify 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  -i ~/.securebackup/identity.txt
```

Output on success:

```text
Backup verification passed.
```

On failure:

```text
Backup verification failed:
- Size mismatch for 000042.chunk: expected 10485760, got 1048576
- Checksum mismatch for 000042.chunk: expected abc..., got def...
```

| Option | Description |
|--------|-------------|
| `-i <file>` | **Required.** Identity private key to decrypt `manifest.age`. |

---

### `restore` — restore a backup

Verifies the full backup first (every chunk, every hash), then concatenates chunks and decrypts the payload.

```bash
# with auto-resolved identity
./dist/securebackup restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  --output ./restored/

# with explicit identity
./dist/securebackup restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  -i ~/.securebackup/identity.txt \
  --output ./restored/
```

If `--output` is a directory (trailing `/` or existing directory), SecureBackup restores the original filename from the decrypted manifest after validating it is safe. If `--output` is a file path, it writes there directly.

Restore rejects:

- Backup IDs that fail UUID v4 validation
- Corrupted or tampered `manifest.age`
- Unsafe filenames in the decrypted manifest (path separators, null bytes, dots only, Windows drive letters)
- Chunks with wrong size or SHA-256

---

### `list` — list backups in storage

```bash
./dist/securebackup list --from local:/mnt/backups
```

Lists UUID directories in the storage root. UUID names are public, so no identity key is needed.

---

## Storage layout

### v2 (current, default)

```text
/mnt/backups/
  {backup_id}/
    locator.json          # public completion marker
    manifest.age          # age-encrypted manifest
    chunks/
      000000.chunk        # encrypted chunk (fixed-size)
      000001.chunk
      000002.chunk
```

**`locator.json`** is a small public file:

```json
{
  "version": 2,
  "app": "securebackup",
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
src/
├── cli.ts                        # citty-based CLI entrypoint + command definitions
├── commands/
│   ├── encrypt.ts                # direct encrypt (single file or --out-dir)
│   ├── decrypt.ts                # direct decrypt (single file or --chunks-dir)
│   ├── upload.ts                 # full backup flow
│   ├── verify.ts                 # backup verification + manifest loading
│   ├── restore.ts                # backup restore with integrity checks
│   ├── keygen.ts                 # age keypair generation
│   ├── list.ts                   # list backups in storage
│   └── storage.ts                # storage URI resolver
├── core/
│   ├── manifest.ts               # Manifest, ManifestV2 types + validation
│   ├── manifest-crypto.ts        # encrypt/decrypt manifest with age
│   ├── locator.ts                # PublicLocatorV2 type + validation
│   ├── chunk.ts                  # file split/concatenation
│   ├── paths.ts                  # chunk name, size parsing, bounds
│   ├── ids.ts                    # UUID v4 generation + validation
│   ├── encrypt.ts                # age-encryption wrapper
│   ├── decrypt.ts                # age-decryption wrapper
│   ├── checksum.ts               # SHA-256 file/bytes hashing
│   └── resolver.ts               # key file resolution
└── storage/
    ├── local.ts                  # LocalStorageBackend
    └── types.ts                  # StorageBackend interface
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
| Chunk size bounds (64 KiB–1 GiB) | Prevents metadata bloat from tiny chunks and memory pressure from 1 GiB+ chunks during `Buffer.alloc`. |

---

## Security model

### What is protected

| Threat | Mitigation |
|--------|------------|
| Storage reads manifest metadata | Manifest encrypted with age. Filenames, sizes, recipient keys, and chunk hashes are ciphertext. |
| Storage tampers with manifest | Decryption fails or decrypted manifest fails `validateManifestV2()`: backup_id mismatch, unsafe filename, invalid chunk hashes, wrong chunk count, non-contiguous indexes. |
| Path traversal in filename | `safeRestoreFilename()` rejects path separators, null bytes, dots-only names, Windows drive letters. `assertInsideDirectory()` double-checks resolved path stays under the restore directory via `resolve()` + `relative()`. |
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
| Streaming memory | Encrypt/decrypt uses `createReadStream` + age `ReadableStream` pipeline. Memory footprint is constant (~100-115 MB) regardless of file size, verified with 500 MB file. |

### Vulnerability audit

See [`VULNERABILITIES.md`](./VULNERABILITIES.md) for the full audit of 10 findings (SB-VULN-001 through SB-VULN-010), all remediated.

Current status:

```text
Tests:   39 pass, 0 fail, 159 expect calls
Audit:   bun audit — 0 known vulnerabilities
Build:   dist/securebackup — 84 modules, 505ms compile
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
| S3 / R2 / S3-compatible backend | `StorageBackend` interface exists. Needs HTTP multipart upload for large files. |
| rclone backend | Write to local staging, rclone syncs. Two tools, one job. |
| Named storage profiles | `--to my-backups` instead of typing URIs. |
| Resumable uploads | Track uploaded chunks, skip on retry. Needs state file in backup directory. |
| Telegram integration | Hermes Agent plugin for backup/restore/verify via Telegram. |
| Per-chunk encryption keys | Each chunk encrypted with a unique key, stored in manifest. Limits blast radius of key exposure. |

---

## License

MIT
