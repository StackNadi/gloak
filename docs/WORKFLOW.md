# Everyday gloak workflows

This guide walks through the tasks most people actually do with a backup tool. Commands assume `gloak` is in your PATH. Every command that talks to storage needs `-R` (where backups live) and usually `-i` (your private key).

> [!WARNING]
> gloak is alpha. Keep another copy of anything critical somewhere else.

## One-time setup

Before the first backup, generate a key pair and create a repository.

```bash
gloak keygen
gloak init -R myremote:backups -r ~/.gloak/recipient.txt
```

`keygen` writes two files under `~/.gloak/`:

- `identity.txt` is your private key. Treat it like a password. Anyone who has it can read every backup.
- `recipient.txt` is your public key. Safe to share; it only lets someone encrypt to you.

`init` creates the storage layout on your remote. You only run it once per remote location.

By default keys live under `~/.gloak`. Set `GLOAK_DIR` (or pass `-o` to `keygen`) to keep them somewhere else. A leading `~` in `GLOAK_DIR` is expanded:

```bash
export GLOAK_DIR=~/backups/gloak-keys
```

Prefer SSH keys for recipients? Pass the public key line directly:

```bash
gloak init -R myremote:backups -r "ssh-ed25519 AAAA... you@laptop"
```

## Back up a folder

Point `backup` at a file or a directory. Directories are walked recursively; empty folders are kept; symlinks are skipped.

```bash
gloak backup ~/Documents -R myremote:backups -i ~/.gloak/identity.txt
```

The command prints a snapshot ID when it finishes. Save it. That ID is how you list, verify, or restore this backup later.

Backup of the same data twice uploads almost nothing the second time. Chunks that already exist on the remote are skipped automatically.

## See what you have backed up

```bash
gloak snapshots -R myremote:backups -i ~/.gloak/identity.txt
```

You get a table like:

```text
UUID                                  KIND  CREATED              HOST     DETAIL
aaaaaaaa-bbbb-4ccc-8ddd-000000000000  tree  2026-10-12T10:00:00Z laptop   12 files from Documents
```

`KIND` is `tree` for folders and `file` for a single-file backup.

## Check a backup before you trust it

Two levels:

```bash
# quick structural check: every snapshot opens, every chunk is present
gloak check -R myremote:backups -i ~/.gloak/identity.txt

# deep check of one snapshot: downloads and decrypts every chunk
gloak verify <snapshot-id> -R myremote:backups -i ~/.gloak/identity.txt
```

Run `check` occasionally. Run `verify` after a big backup, or before you delete the original files.

## Restore

Full restore of a folder snapshot:

```bash
gloak restore <snapshot-id> -R myremote:backups -i ~/.gloak/identity.txt -o ./restored
```

Files land under `./restored/<folder-name>/`. To pull only one subfolder:

```bash
gloak restore <snapshot-id> -R myremote:backups -i ~/.gloak/identity.txt -o ./restored --path subfolder
```

Single-file snapshots restore as one file under the output directory.

## Clean up old backups

Snapshots pile up. `prune` removes old ones and deletes chunks nothing needs anymore.

```bash
# dry run: show what would be deleted, change nothing
gloak prune -R myremote:backups -i ~/.gloak/identity.txt --keep-last 7 --keep-daily 7 --keep-weekly 4

# actually delete
gloak prune -R myremote:backups -i ~/.gloak/identity.txt --keep-last 7 --keep-daily 7 --keep-weekly 4 --yes
```

How the keep flags work:

- `--keep-last N` keeps the N newest snapshots.
- `--keep-daily N` keeps the newest snapshot of each of the last N days that have one.
- `--keep-weekly N` and `--keep-monthly N` work the same way for weeks and months.

At least one keep flag is required. Without `--yes`, prune only prints the plan.

## Optional: notify on Telegram

Set two environment variables and gloak sends a short status report after backup, restore, verify, check, or prune. The message includes the path or snapshot, useful counts, and how long the command took:

```bash
export GLOAK_TELEGRAM_TOKEN="123456:ABC..."
export GLOAK_TELEGRAM_CHAT_ID="your-chat-id"
```

Example message after a backup:

```text
gloak backup completed
Path: /home/you/Documents
Snapshot: 550e8400-e29b-41d4-a716-446655440000
Duration: 42s
```

A notify failure only prints a warning. It never fails the backup itself. The token is never printed.

## If something goes wrong

- Wrong or missing identity: `identity cannot open this repository`. Confirm you are using the key that matches a recipient listed at init time.
- Snapshot not found: check the ID with `gloak snapshots`.
- Corruption during restore or verify: the error starts with `CHUNK CORRUPT`. The bad chunk is named. Keep the original data if you still have it, re-run backup, then prune the damaged snapshot.
- Storage errors: confirm `rclone` is installed and can reach your remote. `gloak setup` installs rclone into `~/.gloak/bin/` if you need it.

## Typical weekly routine

```bash
gloak backup ~/Documents -R myremote:backups -i ~/.gloak/identity.txt
gloak check -R myremote:backups -i ~/.gloak/identity.txt
gloak prune -R myremote:backups -i ~/.gloak/identity.txt --keep-last 7 --keep-daily 7 --yes
```

That is backup, sanity check, and cleanup in three commands.
