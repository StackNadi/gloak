# Vulnerabilities and Security Risks

Audited: 2026-05-14T14:39:32Z
Remediated: 2026-05-14T15:20:00Z

Remediation summary: All 10 findings remediated. SB-VULN-001, 002, 003, 006, 007, 008, 009, and 010 have code-level mitigations and regression tests. SB-VULN-004 is fixed for metadata privacy and arbitrary plaintext manifest tampering by encrypted manifest v2 (`manifest.age`) plus strict decrypted-manifest validation (residual provenance/replay risk remains because recipient-based age encryption is not a writer signature). SB-VULN-005 is fixed by switching `encryptFile`/`decryptFile` from `readFile`-all-at-once to a streaming pipeline via `ReadableStream`; memory usage is now constant (~100 MB) regardless of file size.

This document records security issues and likely future vulnerabilities found during a manual audit of SecureBackup. It covers the current local-backend MVP implementation, not future S3/rclone backends.

## Audit scope

Reviewed areas:

- CLI argument handling and error paths: `src/cli.ts`
- Key generation and key resolution: `src/commands/keygen.ts`, `src/core/resolver.ts`
- Encryption/decryption flow: `src/core/encrypt.ts`, `src/core/decrypt.ts`, `src/commands/encrypt.ts`, `src/commands/decrypt.ts`
- Upload/restore/verify flow: `src/commands/upload.ts`, `src/commands/restore.ts`, `src/commands/verify.ts`
- Chunk handling: `src/core/chunk.ts`
- Local storage backend and destination parsing: `src/storage/local.ts`, `src/storage/types.ts`
- Manifest model: `src/core/manifest.ts`
- Dependency audit: `bun audit --json`

Dependency scan result:

```text
bun audit: 0 known vulnerabilities
```

## Summary

No known dependency CVEs were found, but the current MVP has several design-level risks. The biggest issues are path traversal from untrusted backup IDs/manifests, weak trust assumptions around plaintext mutable manifests, and unbounded memory allocation for large files or chunk sizes.

## Findings

### SB-VULN-001: Path traversal through `backupId` in local backend operations

Severity: High

Status: Fixed

Affected code:

- `src/commands/verify.ts`
- `src/commands/restore.ts`
- `src/storage/local.ts`

Problem:

`backupId` is user-controlled input and is interpolated directly into storage paths:

```ts
`${backupId}/manifest.json`
`${backupId}/chunks/${chunk.name}`
```

The local backend resolves remote paths with:

```ts
join(this.root, remotePath)
```

There is no validation that `backupId` is a UUID and no confinement check ensuring the resolved path stays under the configured storage root.

Impact:

A malicious or mistaken backup ID containing `../` path segments could cause `verify` or `restore` to read files outside the intended local backup root if matching paths exist. This is especially risky if SecureBackup is ever run in automation, with elevated permissions, or against a shared writable backup directory.

Example risky shape:

```text
securebackup verify ../../somewhere --from local:/backups
```

Recommended fix:

- Validate `backupId` before use. For the current design, accept only UUID v4-style IDs.
- Add a safe path resolver in the local backend:
  - resolve `root` to an absolute path
  - resolve the requested target path
  - reject if the target path is outside the root
- Reject absolute remote paths and any remote path segment equal to `..`.

---

### SB-VULN-002: Path traversal through untrusted manifest chunk names

Severity: High

Status: Fixed

Affected code:

- `src/commands/verify.ts`
- `src/commands/restore.ts`
- `src/core/chunk.ts`

Problem:

The manifest is downloaded from storage and parsed without schema validation. `manifest.chunks[].name` is then used in filesystem paths:

```ts
const remotePath = `${backupId}/chunks/${chunk.name}`
const localPath = join(tempDir, chunk.name)
createReadStream(join(chunksDir, chunk.name))
```

If an attacker can modify `manifest.json`, a chunk name such as `../../outside` can escape the intended temp directory or storage prefix.

Impact:

A malicious manifest can potentially cause reads/writes outside intended directories during verify/restore. In the local backend, this could read arbitrary files reachable by the process if paths line up. In future remote backends, it could also request unexpected object keys.

Recommended fix:

- Validate manifest schema before using it.
- Require chunk names to match exactly:

```regex
^\d{6}\.chunk$
```

- Require `chunk.index` to be a non-negative integer.
- Require chunk names to match their index, e.g. index `0` must be `000000.chunk`.
- Reject duplicate indexes and duplicate names.
- Reject manifests where `chunking.total_chunks !== chunks.length`.

---

### SB-VULN-003: Restore output path traversal through `manifest.source.original_filename`

Severity: High

Status: Fixed

Affected code:

- `src/commands/restore.ts`

Problem:

When restore output is a directory, SecureBackup writes to:

```ts
outputFile = join(options.output, manifest.source.original_filename)
```

`manifest.source.original_filename` comes from plaintext `manifest.json` in storage. It is not validated or reduced to a basename during restore.

Impact:

A malicious manifest could set `original_filename` to a traversal path such as:

```text
../../.ssh/authorized_keys
```

If the attacker also provides a valid encrypted payload for the user's recipient, restore could write outside the chosen output directory.

Recommended fix:

- Treat manifest filenames as display metadata, not trusted paths.
- On restore, force:

```ts
safeName = basename(manifest.source.original_filename)
```

- Reject filenames containing path separators, drive prefixes, NUL bytes, `.` or `..`.
- Consider requiring `--output <file>` for restore unless a safe filename passes validation.

---

### SB-VULN-004: Plaintext manifest is mutable and unauthenticated

Severity: Medium-High

Status: Fixed for plaintext metadata privacy and arbitrary manifest-field tampering / Residual provenance and replay risk accepted until signed manifests are added

Affected code:

- `src/commands/upload.ts`
- `src/commands/verify.ts`
- `src/commands/restore.ts`
- `src/core/manifest.ts`

Problem:

The manifest is plaintext and not signed or authenticated. It contains SHA-256 checksums, but those checksums are also inside the same mutable plaintext manifest. A malicious storage provider or attacker with write access can modify chunks and update the manifest checksums to match.

Age encryption authenticates the encrypted payload, so random corruption should fail decryption. However, the current design does not authenticate backup metadata or bind a specific backup ID, filename, timestamp, recipient, and chunk list to a trusted signer.

Impact:

A malicious storage provider can:

- modify metadata such as original filename, size, timestamp, and recipient
- replay an older valid backup under a newer backup ID
- replace a backup with another valid age-encrypted payload encrypted to the same recipient
- exploit path traversal issues if manifest validation is missing

Recommended fix:

Prefer one of these designs:

1. Encrypt the manifest as age data too, leaving only a tiny public locator if needed.
2. Sign the manifest with a local signing key and verify before restore.
3. Store a keyed MAC over the manifest and chunk list using a key not stored in the backup backend.

At minimum:

- validate manifest schema strictly
- store and verify `backup_id` matches the requested backup ID
- reject mismatched `app`, `version`, `storage.layout`, and `encryption.format`
- document that plaintext manifests are not tamper-proof

---

### SB-VULN-005: Unbounded memory usage during encryption, decryption, and chunking

Severity: Medium

Status: Fixed

Affected code:

- `src/core/encrypt.ts` (fixed)
- `src/core/decrypt.ts` (fixed)
- `src/core/chunk.ts` (already chunked)
- `src/core/paths.ts` (bounds enforced)

Problem (before fix):

Encryption and decryption read entire files into memory:

```ts
const plaintext = await readFile(inputFile)
const ciphertext = await readFile(inputFile)
```

This meant a 10 GB file would use 10 GB+ of RAM.

Chunking also allocated one buffer of `chunkSize`:

```ts
const buffer = Buffer.allocUnsafe(chunkSize)
```

`parseSize` allowed very large values as long as they are safe integers and positive.

Fix applied:

- `encryptFile`/`decryptFile` rewritten to use `createReadStream` → `Readable.toWeb()` → age `encrypt(ReadableStream)` → `Readable.fromWeb()` → `pipeline()` to output file. Memory footprint is now constant (~100-115 MB) regardless of file size.
- Chunk size bounds enforced: 64 KiB minimum, 1 GiB maximum via `validateChunkSize()` in `src/core/paths.ts`.
- `Buffer.allocUnsafe` replaced with `Buffer.alloc` in chunking. Verified via test.

Verified with 500 MB file: encrypt RSS 112 MB, decrypt RSS 103 MB. Memory does not scale with file size.

---

### SB-VULN-006: Local backup directory permissions may expose plaintext metadata

Severity: Medium

Status: Fixed

Affected code:

- `src/storage/local.ts`
- `src/commands/upload.ts`

Problem:

The local backend creates directories with default permissions:

```ts
await mkdir(dirname(target), { recursive: true })
```

The encrypted chunks and manifest source files are written with restrictive modes before copy, but the containing directories in the destination backend are governed by process umask/defaults. The manifest is plaintext and can reveal sensitive metadata.

Impact:

On multi-user systems, local backup directories may expose:

- original filename
- original file size
- creation timestamp
- chunk count
- recipient public key

Encrypted chunks remain encrypted, but metadata leakage can still be sensitive.

Recommended fix:

- Create local backend directories with `mode: 0o700` by default.
- Consider writing/copying manifest with `0o600` and explicitly chmod after copy.
- Add a config/flag for shared backup directories if group-readable permissions are desired.
- Consider encrypted manifests for stronger privacy.

---

### SB-VULN-007: Direct chunk decrypt has no manifest-based integrity check

Severity: Medium

Status: Fixed

Affected code:

- `src/commands/decrypt.ts`

Problem:

Direct chunk decrypt reconstructs encrypted data from all files matching:

```regex
^\d{6}\.chunk$
```

It does not verify chunk hashes, expected count, expected encrypted file hash, or a manifest. It relies on age decryption to fail if the final ciphertext is invalid.

Impact:

Accidental missing/reordered/corrupted chunks will likely fail during age decryption, but the user gets weaker diagnostics. If future formats or features are added, this path may become easier to misuse because it bypasses the manifest verification model.

Recommended fix:

- Document direct chunk decrypt as a low-level/manual recovery mode.
- Prefer `restore` for normal backups because it verifies manifest chunk hashes.
- Optionally support `decrypt --chunks-dir <dir> --manifest <manifest.json>` to verify before decrypting.

---

### SB-VULN-008: `verify` does not validate manifest consistency beyond chunk file hashes

Severity: Medium

Status: Fixed

Affected code:

- `src/commands/verify.ts`

Problem:

`verify` checks that listed chunks exist and match listed sizes/hashes, but it does not validate important manifest invariants:

- `manifest.version === 1`
- `manifest.backup_id === requested backupId`
- `manifest.chunking.total_chunks === manifest.chunks.length`
- chunk indexes are contiguous and start at zero
- chunk names match indexes
- no duplicate chunk names or indexes
- `manifest.integrity.encrypted_file_sha256` is valid hex
- `manifest.source.original_filename` is safe metadata

Impact:

Malformed or malicious manifests can pass partial verification or trigger unsafe behavior later during restore.

Recommended fix:

Add a `validateManifest(manifest, requestedBackupId)` function and call it before verify/restore uses any manifest field.

---

### SB-VULN-009: Existing backup IDs are not checked before upload

Severity: Low-Medium

Status: Fixed

Affected code:

- `src/commands/upload.ts`
- `src/storage/local.ts`

Problem:

Backup IDs are random UUIDs, so collisions are extremely unlikely. However, upload does not check whether the destination backup folder already exists before writing chunks and manifest.

Impact:

If a collision, bug, or future user-specified backup ID occurs, upload could overwrite or mix backup data. This is more likely to matter when future backends or import/sync features are added.

Recommended fix:

- Before upload, check `backend.exists(`${backupId}/manifest.json`)` and fail if it exists.
- Make local backend writes use exclusive create semantics where possible.
- Keep manifest-upload-last behavior.

---

### SB-VULN-010: Key material is returned from `generateKeypair`

Severity: Low-Medium

Status: Fixed

Affected code:

- `src/commands/keygen.ts`
- `src/cli.ts`

Problem:

`generateKeypair` returns both public and private key material:

```ts
return { keyDir, identityFile, recipientFile, identity, recipient }
```

The CLI currently prints only the recipient and file paths, not the private identity. However, returning the private key makes accidental logging or exposure easier for future callers/tests/integrations.

Impact:

Future code could accidentally log or serialize the returned `identity` field.

Recommended fix:

- Change `generateKeypair` to return only paths and recipient by default.
- If tests need to verify private key generation, read the identity file directly in test code.
- If returning identity is necessary, clearly name it as sensitive and avoid passing it to CLI output layers.

## Remediation implemented

Code-level changes added after this audit:

- UUID v4 validation for `backupId` before `verify`, `restore`, and manifest load.
- Local backend root confinement with absolute-path, `..`, and path-separator checks for remote paths.
- Strict manifest validation via `validateManifest(...)`: app/version/layout/encryption invariants, backup ID match, safe original filename, chunk name/index/hash/size validation, duplicate detection, and contiguous chunk indexes.
- Restore directory output now uses validated safe filenames and checks the resolved output stays inside the chosen directory.
- CLI `--chunk-size` parsing now rejects values below 64KiB or above 1GiB; chunking no longer uses `Buffer.allocUnsafe`.
- Local backend creates private directories (`0o700`) and copied files (`0o600`).
- Direct chunk decrypt now requires `--manifest` / `manifestFile` for chunk and encrypted payload integrity checks.
- Upload checks for an existing manifest before writing a new backup and local uploads refuse overwriting existing remote files.
- `generateKeypair()` no longer returns private identity material; callers read the identity file only if they explicitly need it.

Verification:

```text
bun test: 37 pass, 0 fail
bun audit: No vulnerabilities found
bun run build: succeeded
```

Residual risk:

- SB-VULN-004 now uses encrypted manifest v2 (`manifest.age`) and public `locator.json`. Plaintext sensitive metadata is no longer stored by new uploads, and tampered manifest ciphertext fails age decryption. Recipient encryption still does not prove writer identity, so signed manifests remain future work if provenance/auditability becomes required.

## Positive findings

The audit also found several good security properties already present:

- `keygen` writes `identity.txt` with `0o600` and key directory with `0o700`.
- `keygen` refuses to overwrite existing key files.
- CLI does not print the private identity key during `keygen`.
- Backup folders use random UUIDs instead of original filenames.
- Manifest is uploaded after chunks, reducing false-complete partial uploads.
- Restore verifies chunk hashes and encrypted-file hash before decrypting in the normal `restore` path.
- CLI error handling avoids stack trace leakage for normal command failures.
- Dependency audit currently reports zero known vulnerabilities.

## Recommended fix order

1. Add safe path resolution and UUID validation for `backupId`.
2. Add strict manifest schema validation before `verify` and `restore` use manifest fields.
3. Sanitize restore filenames with `basename` and reject path separators.
4. Add local backend root confinement checks.
5. Add chunk-size limits and plan streaming crypto for large files.
6. Decide whether manifests should be encrypted or signed before adding remote backends.
7. Tighten local backend permissions.
8. Reduce accidental exposure risk from `generateKeypair` return values.

## Suggested regression tests

Add tests for:

- rejecting backup IDs containing `../`, `/`, `\`, or non-UUID strings
- rejecting manifest chunk names that do not match `^\d{6}\.chunk$`
- rejecting manifests where `backup_id` does not match requested ID
- rejecting duplicate chunk indexes/names
- restoring to a directory with malicious `original_filename` does not escape the output directory
- local backend refuses remote paths that resolve outside its root
- huge `--chunk-size` is rejected before allocation
- `generateKeypair` API does not expose private key material unless explicitly requested
