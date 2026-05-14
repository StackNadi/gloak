# SecureBackup

SecureBackup is a Bun CLI for encrypting files with `age-encryption`.

It has two jobs:

1. Encrypt or decrypt a file directly.
2. Create a backup made of encrypted chunks, with a manifest and a local storage backend.

The direct command is for quick work. The backup command is for cloud storage, where a single huge encrypted blob is annoying to upload, retry, or verify.

## Install

```bash
bun install
```

Build the standalone binary:

```bash
bun build src/cli.ts --compile --outfile dist/securebackup
```

Run tests:

```bash
bun test
```

## Create an age key

SecureBackup uses the `age-encryption` package. Generate an identity and recipient like this:

```bash
bun -e 'import * as age from "age-encryption"; const id = await age.generateIdentity(); console.log("identity=", id); console.log("recipient=", await age.identityToRecipient(id))'
```

Keep the identity private. Use the recipient when encrypting.

## Encrypt one file

This writes one `.age` file. No chunks. No manifest. No storage backend.

```bash
./dist/securebackup encrypt ./secret.txt \
  --recipient age1... \
  --output ./secret.txt.age
```

Decrypt it later:

```bash
./dist/securebackup decrypt ./secret.txt.age \
  --identity AGE-SECRET-KEY-... \
  --output ./secret.txt
```

## Encrypt and split into chunks

Use this when you want encrypted chunks but do not want the full backup layout.

```bash
./dist/securebackup encrypt ./video.tar \
  --recipient age1... \
  --out-dir ./video-encrypted-chunks \
  --chunk-size 20MB
```

The output directory contains files like this:

```text
000000.chunk
000001.chunk
000002.chunk
```

Decrypt those chunks back into one file:

```bash
./dist/securebackup decrypt \
  --chunks-dir ./video-encrypted-chunks \
  --identity AGE-SECRET-KEY-... \
  --output ./video.tar
```

This mode encrypts the full file first, then splits the encrypted output. Decrypt does the reverse: it joins the chunks in filename order, then decrypts the joined encrypted payload.

It does not create or read `manifest.json`. If you need restore metadata and verification later, use `upload` instead.

## Upload a backup to local storage

`upload` is the full backup flow. It creates a UUID folder, chunks the encrypted payload, writes checksums, and uploads `manifest.json` last.

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

The storage layout is:

```text
/mnt/backups/
  7f91c6c7-7a0b-44aa-ae23-997b60e4e998/
    manifest.json
    chunks/
      000000.chunk
      000001.chunk
```

The folder name is a UUID, not the original filename. The manifest still contains the original filename because restore needs it.

## Verify a backup

```bash
./dist/securebackup verify 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups
```

`verify` checks that the manifest exists, every chunk exists, chunk sizes match, and SHA-256 hashes match.

## Restore a backup

```bash
./dist/securebackup restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  --identity AGE-SECRET-KEY-... \
  --output ./restored/
```

If `--output` is a directory, SecureBackup restores the original filename from `manifest.json`.

## List backups

```bash
./dist/securebackup list --from local:/mnt/backups
```

## Current scope

Implemented:

- Bun CLI
- standalone binary build
- `age-encryption` recipient mode
- direct `encrypt` and `decrypt`
- direct encrypted chunk split with `encrypt --out-dir`
- direct encrypted chunk restore with `decrypt --chunks-dir`
- local backend
- `upload`, `restore`, `verify`, and `list`
- UUID backup folders
- fixed-width chunk names
- chunk SHA-256 checks
- encrypted payload SHA-256 checks
- manifest upload after chunks

Not built yet:

- S3 or R2 backend
- rclone backend
- named storage profiles
- resumable uploads
- encrypted manifest metadata
- streaming pipeline
- Telegram integration

## Notes

`encrypt --out-dir` is intentionally bare. It gives you encrypted chunks and nothing else.

`decrypt --chunks-dir` is just as bare. It joins files named `000000.chunk`, `000001.chunk`, and so on, then decrypts the joined payload. It trusts the directory contents because this mode has no manifest.

`upload` is safer for backups because it keeps the metadata needed to verify and restore the file. Use that when the chunks are going to cloud storage and you want less future pain. Future-you is already tired. Give them the manifest.
