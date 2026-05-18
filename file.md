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

By default the keys go to `~/.securebackup/`. To use a custom directory:

```bash
./dist/securebackup keygen --output /path/to/keys
# or
./dist/securebackup keygen -o /path/to/keys
```

SecureBackup writes these files:

```text
~/.securebackup/
  identity.txt      (private key — keep this secret)
  recipient.txt     (public key — safe to share)
```

>`identity.txt` is the private key. Keep it private. `recipient.txt` is safe to use for encryption commands.

After you run `keygen` once, `-i` and `-r` are optional. SecureBackup reads the key files from `~/.securebackup/` automatically when you skip those flags.

Encrypt:

```bash
./dist/securebackup encrypt ./secret.txt --output ./secret.txt.age
```

Decrypt:

```bash
./dist/securebackup decrypt ./secret.txt.age --output ./secret.txt
```

You can still pass `-r <file>` or `-i <file>` to override with a different key file (like SSH's `-i` flag). The saved files are only a fallback.

```bash
# use a different recipient file
./dist/securebackup encrypt ./secret.txt -r /path/to/other-recipient.txt --output ./secret.txt.age

# use a different identity file
./dist/securebackup decrypt ./secret.txt.age -i /path/to/other-identity.txt --output ./secret.txt
```

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
  -r /path/to/recipient.txt \
  --output ./secret.txt.age
```

Decrypt it later:

```bash
./dist/securebackup decrypt ./secret.txt.age --output ./secret.txt
```

Or with an explicit identity file:

```bash
./dist/securebackup decrypt ./secret.txt.age \
  -i /path/to/identity.txt \
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
  -r /path/to/recipient.txt \
  --out-dir ./video-encrypted-chunks \
  --chunk-size 20MB
```

The output directory contains files like this:

```text
000000.chunk
000001.chunk
000002.chunk
```

Direct chunk decrypt is intentionally restricted. `decrypt --chunks-dir` requires a manifest file so chunk integrity can be verified before decryption:

```bash
./dist/securebackup decrypt \
  --chunks-dir ./video-encrypted-chunks \
  --manifest ./manifest.json \
  -i /path/to/identity.txt \
  --output ./video.tar
```

Direct `encrypt --out-dir` does not create a backup manifest. If you need restore metadata and verification later, use `upload` instead.

## Upload a backup to local storage

`upload` is the full backup flow. It creates a UUID folder, chunks the encrypted payload, writes checksums into an age-encrypted `manifest.age`, and uploads public `locator.json` last.

If you ran `keygen`:

```bash
./dist/securebackup upload ./backup.tar --to local:/mnt/backups
```

Or with an explicit recipient:

```bash
./dist/securebackup upload ./backup.tar \
  -r /path/to/recipient.txt \
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
    locator.json
    manifest.age
    chunks/
      000000.chunk
      000001.chunk
```

The folder name is a UUID, not the original filename. Sensitive metadata such as the original filename, chunk hashes, recipient, and payload hash lives inside encrypted `manifest.age`; `locator.json` is only a public completion marker.

## Verify a backup

```bash
./dist/securebackup verify 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  -i /path/to/identity.txt
```

`verify` decrypts `manifest.age`, validates it, checks every chunk exists, verifies chunk sizes and SHA-256 hashes, then verifies the concatenated encrypted payload hash.

## Restore a backup

If you ran `keygen`:

```bash
./dist/securebackup restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  --output ./restored/
```

Or with an explicit identity file:

```bash
./dist/securebackup restore 7f91c6c7-7a0b-44aa-ae23-997b60e4e998 \
  --from local:/mnt/backups \
  -i /path/to/identity.txt \
  --output ./restored/
```

If `--output` is a directory, SecureBackup restores the original filename from decrypted `manifest.age` after validating that it is a safe filename.

## List backups

```bash
./dist/securebackup list --from local:/mnt/backups
```

## Current scope

Implemented:

- Bun CLI
- standalone binary build
- key generation with `securebackup keygen` (custom `--output` / `-o`)
- auto key resolution from `~/.securebackup/`
- SSH-style `-i <file>` and `-r <file>` flag overrides
- `age-encryption` recipient mode
- direct `encrypt` and `decrypt`
- direct encrypted chunk split with `encrypt --out-dir`
- direct encrypted chunk restore with `decrypt --chunks-dir --manifest`
- local backend
- `upload`, `restore`, `verify`, and `list`
- UUID backup folders
- fixed-width chunk names
- chunk SHA-256 checks
- encrypted payload SHA-256 checks
- manifest upload after chunks
- encrypted manifest metadata (`manifest.age`) with public `locator.json` completion marker

Not built yet:

- S3 or R2 backend
- rclone backend
- named storage profiles
- resumable uploads
- streaming pipeline
- Telegram integration

## Notes

`encrypt --out-dir` is intentionally bare. It gives you encrypted chunks and nothing else.

`decrypt --chunks-dir` is a manual recovery path and requires a manifest so it can verify chunk integrity before decrypting. Use `upload`/`restore` for normal backups; those commands use encrypted `manifest.age` automatically.

`upload` is safer for backups because it keeps the metadata needed to verify and restore the file. Use that when the chunks are going to cloud storage and you want less future pain. Future-you is already tired. Give them the manifest.
