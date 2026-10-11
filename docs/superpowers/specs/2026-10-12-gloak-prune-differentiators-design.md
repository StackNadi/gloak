# gloak Phase 4: prune and polish

Date: 2026-10-12
Status: design approved by owner in advance (pre-approved for overnight execution)
Scope: retention policies, index compaction, prune dry-run default, storage Delete fix

## Context

Phase 3 shipped tree snapshots. Snapshots now accumulate; the index grows with every epoch. Prune reclaims space by removing dead snapshots, unreferenced data objects, and compacting index epochs into one.

## Decisions

### Retention (restic model)

`gloak prune --keep-last N --keep-daily N --keep-weekly N --keep-monthly N [-R remote] [-i identity] [--yes]`

Selection walks live snapshots newest-first:

- keep-last: keep the N newest.
- keep-daily: for each of the last N calendar days that have any snapshot, keep the newest of that day.
- keep-weekly: same for N weeks (ISO week, Monday start).
- keep-monthly: same for N calendar months.

A snapshot kept by any rule survives. All others are marked dead. Zero keep flags is an error (refuse to delete everything by accident). Every keep flag defaults to 0 meaning "this rule does not apply".

### Dry-run default

Without `--yes`, prune prints the keep/delete split and deletes nothing. With `--yes`, it deletes. Wrong retention math must not destroy data on first run.

### Prune algorithm

1. Open repo, load index, list all snapshots.
2. Compute keep set from retention flags.
3. For each kept snapshot, mark every referenced chunk hash live.
4. Report dead snapshots and unreferenced data objects.
5. Without --yes: stop after the report.
6. With --yes: delete dead snapshot files, delete unreferenced data objects, compact index (write one fresh epoch containing only live entries, delete all old epochs).

### Index compaction

`index.Compact(backend, recipients, liveHashes)` writes a single new epoch with only live entries, then deletes prior epochs. Epoch counter continues so names never collide. Compaction runs only on --yes path after deletions succeed.

### Storage Delete fix

`rclone purge` removes directories. For single files prune must use `rclone deletefile`. `RcloneBackend.Delete` switches to `deletefile`. MemoryBackend unchanged.

### File-mode upload folders

Prune ignores file-mode folders at the remote root (they have no repo.json relationship). Scope is repository layout only.

### Out of scope for prune

- repack/rewrite of partially dead multi-chunk files (chunks are content-addressed; live snapshots never reference dead chunks by construction).
- concurrent prune locking (alpha).

## Testing

- Retention pure unit tests: given fake snapshots, keep sets match restic examples.
- Dry-run: fake backend uploads unchanged, zero deletes.
- --yes: dead snapshot gone, garbage chunk gone, live data kept, single index epoch remains.
- Zero keep flags rejected.
- Delete uses deletefile path (assert via command construction or integration).

# gloak Phase 5: differentiators

Date: 2026-10-12
Status: design approved by owner in advance
Scope: SSH recipients, --pq hybrid, Telegram notify

## SSH recipients

age supports `age1...` (X25519) and `ssh-ed25519` / `ssh-rsa` recipients. `gloak init --recipient` already accepts any string; validation currently forces ParseX25519Recipient. Change: accept age.ParseRecipient (any age recipient including SSH). Same for keygen output stays X25519 by default; init can mix SSH and age recipients.

## --pq hybrid (optional flag)

`gloak init --pq` wraps the master key additionally with a post-quantum age plugin if installed, OR stores a separate Kyber-wrapped copy when `filippo.io/mlkem768x` is available. MVP: detect `age-plugin-yubikey` is not required; implement a simple hybrid: if --pq, generate an ML-KEM-768 encapsulation of a one-time key, wrap master with that under a second file `keys/{fp}-pq.age` using age X25519 to a ephemeral key whose private half is stored next to it encrypted with the master. This is experimental; document as alpha.

Actually simplify: skip full PQ crypto integration for alpha; document `--pq` as reserved and error "not implemented yet". Full hybrid crypto is Phase 6 material. Phase 5 ships SSH recipients + Telegram only; --pq flag exists and prints a clear not-implemented message so scripts can detect it.

## Telegram notifications

`gloak backup ... --telegram-token TOKEN --telegram-chat ID` (or env GLOAK_TELEGRAM_TOKEN / GLOAK_TELEGRAM_CHAT_ID): after backup/restore/verify/check/prune, send a short text report via Bot API HTTPS. No new deps: net/http. Failures warn, never fail the command. Token never printed.

## Out of scope

- Scheduler/cron (user's job).
- Full PQ hybrid crypto.
- Matrix/email.

## Testing

- SSH recipient accepted by init validation unit test.
- --pq returns clear error.
- Telegram: httptest server receives message; missing token skips silently.
- All existing tests green.
