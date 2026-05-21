# SecureBackup

> Go single-binary CLI for age-encrypted, chunked, verified backups.

Encrypt files, split into 20MB chunks with SHA-256 checks, upload via rclone, verify, restore. Metadata (filenames, sizes, recipients, hashes) lives inside an age-encrypted manifest. Storage never sees plaintext metadata.

```text
keygen → encrypt → chunk → encrypt manifest → upload → verify → restore
```

---

## Quick start

```bash
# build standalone binary
go build -o securebackup_cli .

# generate keys (saved to ~/.securebackup/ automatically)
./securebackup_cli keygen

# encrypt + chunk + upload
./securebackup_cli upload ./secret.tar --remote myremote:backup_folder

# verify integrity
./securebackup_cli verify <uuid> --remote myremote:backup_folder

# restore
./securebackup_cli restore <uuid> --remote myremote:backup_folder --output-dir ./restored/
```

---

## Why this exists

Most backup tools either:

1. Encrypt a single blob — good luck retrying after a network failure at byte 47 GB of 50.
2. Split into chunks without integrity metadata — you find out a chunk is corrupt during restore, with no manifest to guide repair.
3. Store metadata in plaintext — filenames, sizes, recipient keys, and chunk hashes visible to anyone who reads the storage.
4. Pull in rclone, restic rest-server, or S3 libraries when you just need local filesystem or a mounted remote.

SecureBackup is the boring correct version: age encryption, fixed-size 20MB chunks, deterministic manifests, integrity checks on every chunk *and* the full payload, and an encrypted manifest envelope so storage never sees filenames or hashes.

---

## Install

```bash
go mod download
```

Build the standalone binary:

```bash
go build -o securebackup_cli .
```

Produces `securebackup_cli` — a single compiled binary. Drop it into containers, cron jobs, or VPS rootfs without needing Node, Bun, or Python installed.

Run tests:

```bash
go test ./...
```

---

## Key generation

Generate a keypair once:

```bash
./securebackup_cli keygen
```

Default location is `~/.securebackup/`:

```text
~/.securebackup/
  identity.txt      # private key — keep this secret
  recipient.txt     # public key — safe to share
```

Use a custom directory:

```bash
./securebackup_cli keygen --output-dir /path/to/keys
./securebackup_cli keygen -o /path/to/keys
```

After `keygen` runs once, `-i` (identity) and `-r` (recipient) flags become optional. SecureBackup reads from `~/.securebackup/` automatically.

### Key flags

- `-r <file>` / `--recipient` — Recipient public key file or raw `age1...` string. Overrides `~/.securebackup/recipient.txt`.
- `-i <file>` / `--identity` — Identity private key file. Overrides `~/.securebackup/identity.txt`.

---

## Commands

### `upload` — full encrypted backup flow

Creates a UUID folder, encrypts the input with age, splits the encrypted payload into 20MB chunks, computes SHA-256 for each chunk and the overall payload, encrypts all metadata into `manifest.age`, and uploads in order: chunks first, manifest second, locator last.

```bash
# using auto-resolved keys from ~/.securebackup/
./securebackup_cli upload ./backup.tar --remote myremote:backup_folder

# with explicit recipient file
./securebackup_cli upload ./backup.tar -r /path/to/recipient.txt --remote myremote:backup_folder

# with raw recipient key
./securebackup_cli upload ./backup.tar -r age1xxxxxxxx --remote myremote:backup_folder
```

Output:

```text
 INFO  Starting UPLOAD process...
 INFO  Using recipient from: /home/user/.securebackup/recipient.txt
 INFO  Target File      : ./backup.tar
 INFO  Destination      : myremote:backup_folder
  OK   Backup ID: 7f91c6c7-7a0b-44aa-ae23-997b60e4e998
      Encrypting & Uploading... ████████████████████████ 100%
 INFO  Building and encrypting manifest...
  OK   Backup completed successfully!
       Save this UUID for restoration: 7f91c6c7-7a0b-44aa-ae23-997b60e4e998
```

- `--remote` (required) — Target rclone destination (`remote:folder` or absolute local path `/mnt/backups`).
- `-r` / `--recipient` — (optional) Recipient key or file. Auto-resolves from `~/.securebackup/recipient.txt`.

The locator is uploaded last. If the upload is interrupted, the backup directory exists but has no `locator.json`. Upload refuses to overwrite on retry, so partial uploads never look complete.

---

### `verify` — verify backup integrity

Downloads and decrypts `manifest.age`, checks every chunk exists with matching SHA-256, then checks the concatenated encrypted payload hash against the manifest.

```bash
# auto-resolved identity from ~/.securebackup/
./securebackup_cli verify 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --remote myremote:backup_folder

# with explicit identity
./securebackup_cli verify 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --remote myremote:backup_folder \
  -i ~/.securebackup/identity.txt
```

Output on success:

```text
 INFO  Starting verification for UUID: 7f91c6c7-7a0b-44aa-ae23-997b60e4e998
 INFO  Decrypting manifest...
  OK   Manifest valid! Verifying 3 chunks...
      Verifying Chunks... ████████████████████████ 100%
  OK   VERIFIED! All chunks are healthy and checksums match 100%.
```

On failure:

```text
ERROR CHUNK CORRUPT: Hash chunk_00042 mismatch!
```

- `-i` / `--identity` — (optional) Identity private key. Auto-resolves from `~/.securebackup/identity.txt`.

---

### `restore` — restore a backup

Verifies the full backup first (every chunk, every hash), then concatenates chunks and decrypts the payload into the output directory.

```bash
# auto-resolved identity from ~/.securebackup/
./securebackup_cli restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --remote myremote:backup_folder \
  --output-dir ./restored/

# with explicit identity
./securebackup_cli restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --remote myremote:backup_folder \
  -i ~/.securebackup/identity.txt \
  --output-dir ./restored/
```

Output:

```text
 INFO  Starting restore for UUID: 7f91c6c7-7a0b-44aa-ae23-997b60e4e998
 INFO  Downloading encrypted manifest...
 INFO  Decrypting manifest...
  OK   Manifest valid! Restoring file: backup.tar (104857600 bytes)
      Downloading & Decrypting... ████████████████████████ 100%
  OK   Restore completed successfully! File saved to: ./restored/backup.tar
```

The original filename is recovered from the decrypted manifest.

Restore rejects:

- Corrupted or tampered `manifest.age` (decryption fails)
- Chunks with SHA-256 mismatch
- Overall payload hash mismatch

---

## Storage layout

```text
{remote}/
  {uuid}/
    locator.json       # public completion marker
    manifest.age       # age-encrypted manifest (all metadata)
    chunk_00000        # first 20MB encrypted chunk
    chunk_00001
    chunk_00002
    ...
```

**`locator.json`** — small public file:

```json
{
  "version": "1.0",
  "uuid": "7f91c6c7-7a0b-44aa-ae23-997b60e4e998"
}
```

No sensitive metadata. No filename, no recipient, no chunk hashes. It exists so tools can detect backup completion without decrypting anything.

**`manifest.age`** — age-encrypted JSON. Once decrypted:

- `version` — Manifest version (`"1.0"`)
- `uuid` — Backup UUID
- `original_name` — Basename of backed-up file
- `original_size` — Original file size in bytes
- `created_at` — RFC3339 UTC timestamp
- `recipient` — Age public key used for encryption
- `payload_encrypted_hash` — SHA-256 of the concatenated encrypted chunks
- `chunks[]` — Array of per-chunk metadata:
  - `index` — Chunk index (0-based)
  - `name` — Chunk filename (`chunk_00000`)
  - `size` — Chunk byte size
  - `sha256` — SHA-256 of the encrypted chunk content

---

## Architecture

```text
securebackup/
├── main.go                           # kong CLI: keygen, upload, restore, verify
├── internal/
│   ├── core/
│   │   ├── chunk.go                  # StreamChunker: 20MB fixed-size, SHA-256 per chunk
│   │   └── manifest.go              # Manifest/Locator structs, EncryptManifest, SerializeLocator
│   ├── crypto/
│   │   ├── keygen.go                 # GenerateKey(): X25519 identity + recipient
│   │   └── stream.go                # EncryptWriter() / DecryptReader() wrapping filippo.io/age
│   ├── flow/
│   │   ├── upload.go                 # Upload pipeline: ProgressReader → EncryptWriter → StreamChunker → rclone rcat
│   │   ├── restore.go               # Restore pipeline: download manifest → verify chunks → decrypt → write file
│   │   └── verify.go                # Verify pipeline: download manifest → verify chunk hashes → verify payload hash
│   └── storage/
│       └── rclone.go                 # RcloneBackend: Upload/Download via stdin/stdout pipes
├── go.mod                            # filippo.io/age, kong, pterm, google/uuid
└── go.sum
```

### Data flow

```text
upload:
  open file
    → ProgressReader (pterm progress bar)
    → age EncryptWriter (via io.Pipe)
    → SHA-256 tee for overall payload hash
    → StreamChunker (20MB per chunk, SHA-256 each)
    → rclone rcat (chunk_00000, chunk_00001, ...)
    → EncryptManifest → upload manifest.age
    → upload locator.json (last, as completion marker)

verify:
  rclone cat → download manifest.age
    → age DecryptReader
    → parse Manifest JSON
    → for each chunk: rclone cat → SHA-256 → compare with manifest
    → overall payload SHA-256 → compare with manifest

restore:
  (same as verify for integrity)
    → rclone cat → download chunks
    → SHA-256 each chunk (verify before decrypt)
    → io.Pipe → age DecryptReader
    → ProgressReader → io.Copy to output file
```

### Key design decisions

- **Recipient-mode age encryption** — One identity decrypts. No passphrase to remember or leak.
- **Fixed 20MB chunks** — Deterministic boundaries. `const ChunkSize = 20 * 1024 * 1024`. Simplifies verification and storage layout.
- **Encrypted manifest** — Storage never sees filenames, sizes, or hashes. Only backup UUIDs and chunk object names are visible.
- **Locator uploaded last** — Atomicity marker. If upload is interrupted, no `locator.json` — upload refuses to overwrite on retry.
- **Identity required for verify/restore** — Manifest is encrypted. You need the identity key to read it. This is a feature, not a bug.
- **SHA-256 at chunk + payload** — Corruption detected per-chunk before decrypt, and for the overall payload.
- **Zero disk footprint** — `io.Pipe` + `rclone rcat` via stdin/stdout. Chunks are never written to local disk. RAM stays under 50MB regardless of file size.
- **Rclone as sole backend** — No direct S3/GDrive code. Everything goes through rclone CLI. Supports 40+ storage backends via rclone config.
- **Default key paths** — Auto-reads from `~/.securebackup/identity.txt` and `~/.securebackup/recipient.txt`. Flags override.

---

## Security model

### What is protected

- **Storage reads manifest metadata** — Manifest encrypted with age. Filenames, sizes, recipient keys, and chunk hashes are ciphertext.
- **Storage tampers with manifest** — Decryption fails or decrypted manifest fails validation.
- **Storage replaces a chunk** — SHA-256 verification catches size or content mismatch at chunk and payload level.
- **Storage replays an old locator** — Locator UUID must match the requested backup ID. Manifest UUID must also match after decrypt.
- **Unauthorized upload** — Upload requires an age recipient public key.
- **Duplicate backup overwrite** — Upload does not overwrite existing backup directories.

### What is NOT protected

- **No writer provenance** — Age encryption proves the encryptor knows the recipient, not who they are. A malicious storage provider with access to the recipient file could upload a fake backup that decrypts.
- **Metadata side channels** — Backup UUIDs visible in storage listings. Chunk count and sizes visible from object metadata. File size inferred from chunk count x chunk size (minus padding).
- **In-transit encryption** — Transport security is the storage backend's job. Local filesystem has none; S3/R2 would use HTTPS via rclone.
- **Streaming memory** — Encrypt/decrypt uses Go `io.Pipe`, `io.MultiWriter`, and `filippo.io/age` streams chunks directly to `rclone` standard input. Zero disk footprint for chunks, stable RAM usage under 50MB regardless of file size.

### Vulnerability audit

See [`VULNERABILITIES.md`](./VULNERABILITIES.md) for the full audit (untracked in git).

```text
Audit:   govulncheck ./... — 0 known vulnerabilities
Build:   Go 1.22+ standalone binary
```

---

## Not built yet

Deferred intentionally:

- **Named storage profiles** — `--remote my-backups` instead of typing URIs.
- **Resumable uploads** — Track uploaded chunks, skip on retry. Needs state file in backup directory.

---

## License

MIT
