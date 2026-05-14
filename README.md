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

Generate a keypair once:

```bash
./dist/securebackup keygen
```

SecureBackup writes the files here:

```text
~/.securebackup/
  identity.txt
  recipient.txt
```

>`identity.txt` is the private key. Keep it private. `recipient.txt` is safe to use for encryption commands.

After you run `keygen` once, `--recipient` and `--identity` are optional. SecureBackup reads the key files from `~/.securebackup/` automatically when you skip those flags.

Encrypt:

```bash
./dist/securebackup encrypt ./secret.txt --output ./secret.txt.age
```

Decrypt:

```bash
./dist/securebackup decrypt ./secret.txt.age --output ./secret.txt
```

You can still pass `--recipient` or `--identity` explicitly if you want to use a different key or override the default. The saved files are only a fallback.

Keep the identity file out of git, screenshots, logs, pastebins, and any group chat where optimism goes to die.

## Encrypt one file

This writes one `.age` file. No chunks. No manifest. No storage backend.

If you ran `keygen` already, just pass the file and output path:

```bash
./dist/securebackup encrypt ./secret.txt --output ./secret.txt.age
```

You can still override the recipient:

```bash
./dist/securebackup encrypt ./secret.txt \
  --recipient age1... \
  --output ./secret.txt.age
```

Decrypt it later:

```bash
./dist/securebackup decrypt ./secret.txt.age --output ./secret.txt
```

Or with an explicit key:

```bash
./dist/securebackup decrypt ./secret.txt.age \
  --identity AGE-SECRET-KEY-... \
  --output ./secret.txt
```

> [!WARNING]
> Do not let the intrusive thought win. Never `cat` encrypted `.age` files or chunk files. They are binary, and your terminal did not sign up for that nonsense.

## Encrypt and split into chunks

Use this when you want encrypted chunks but do not want the full backup layout.

If you ran `keygen`:

```bash
./dist/securebackup encrypt ./video.tar \
  --out-dir ./video-encrypted-chunks \
  --chunk-size 20MB
```

Or with an explicit recipient:

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

If you ran `keygen`:

```bash
./dist/securebackup upload ./backup.tar --to local:/mnt/backups
```

Or with an explicit recipient:

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

If you ran `keygen`:

```bash
./dist/securebackup restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  --output ./restored/
```

Or with an explicit identity:

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
- key generation with `securebackup keygen`
- auto key resolution from `~/.securebackup/`
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
