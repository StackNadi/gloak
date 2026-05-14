# SecureBackup CLI

Bun-based single-binary CLI for encrypted cloud-safe backups.

It follows `FEATURE_SPEC.md`:

1. Encrypt input file with `age-encryption`.
2. Split encrypted payload into fixed-size chunks, default `20MB`.
3. Store backup under a UUID folder.
4. Upload chunks first.
5. Upload `manifest.json` last.
6. Restore by downloading manifest/chunks, verifying SHA-256, concatenating, and decrypting.

## Requirements

- Bun 1.x

Install dependencies:

```bash
bun install
```

## Test

```bash
bun test
```

## Build single binary

```bash
bun build src/cli.ts --compile --outfile dist/securebackup
```

## Generate an age identity/recipient

The CLI uses the `age-encryption` library. For now, generate keys from a tiny Bun one-liner:

```bash
bun -e 'import * as age from "age-encryption"; const id = await age.generateIdentity(); console.log("identity=", id); console.log("recipient=", await age.identityToRecipient(id))'
```

Keep the `identity` private. Use the `recipient` for upload.

## Upload

```bash
./dist/securebackup upload ./backup.tar \
  --recipient age1... \
  --to local:/mnt/backups
```

Example output:

```text
Backup uploaded successfully.

Backup ID:
7f91c6c7-7a0b-44aa-ae23-997b60e4e998

Chunks:
205

Remote:
local:/mnt/backups/7f91c6c7-7a0b-44aa-ae23-997b60e4e998
```

## Verify

```bash
./dist/securebackup verify 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups
```

## Restore

```bash
./dist/securebackup restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  --identity AGE-SECRET-KEY-... \
  --output ./restored/
```

If `--output` is an existing directory or ends with `/`, SecureBackup restores using the original filename from `manifest.json`.

## Current v0.1 scope

Implemented:

- Bun CLI scaffold
- Single binary build
- Local backend
- `upload`
- `restore`
- `verify`
- `list`
- UUID backup folders
- `manifest.json`
- 20 MiB default chunking
- chunk SHA-256 verification
- encrypted payload SHA-256 verification
- `age-encryption` integration

Not implemented yet:

- S3-compatible backend
- Rclone backend
- storage profiles
- resumable upload
- encrypted metadata
- streaming pipeline
- Telegram integration
