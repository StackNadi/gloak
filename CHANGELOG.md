# Changelog

## Unreleased

### Added

- `gloak init` creates a repository: a wrapped master key per recipient under `keys/`, a `repo.json` manifest, format 2.0.
- `gloak backup` uploads content-addressed encrypted chunks under `data/{hh}/`, records them in an encrypted epoch index under `index/`, and writes a snapshot under `snapshots/`. Repeat backups skip chunks already in the index. Chunks are sealed with zstd and then XChaCha20-Poly1305 under HKDF-derived keys.
- Snapshots are encrypted to every recipient listed at init time. Any matching identity can verify or restore.
- `gloak upload <file> -R <remote>` writes a self-contained backup folder `{uuid}/` at the remote root (sealed chunks, multi-recipient `manifest.age` with embedded master key, public `locator.json`). No repository and no `init` required. Manifest version `file-2`.
- `restore` and `verify` auto-detect file-mode folders by `locator.json` and fall back to repository layout otherwise.
- `backup` splits files with content-defined chunking (gear hash). Chunk bounds `chunk_min`/`chunk_avg`/`chunk_max` are stored in `repo.json` at init; flags `--chunk-min`, `--chunk-avg`, `--chunk-max` override the defaults (512 KiB / 1 MiB / 8 MiB). A repeat backup of unchanged content uploads nothing.
- `gloak check` reports structural consistency: every snapshot decrypts, every referenced chunk exists in `data/`, and unreferenced objects are listed as garbage. It does not download chunk payloads; use `verify` for a deep pass.

### Changed

- `verify` and `restore` read the repository layout. Corruption errors keep the CHUNK CORRUPT prefix.
- Repository backup no longer uses fixed 20 MiB chunks. Phase 1 repositories with `chunk_size` in `repo.json` cannot be opened; re-run `gloak init`.

### Removed

- The `resume` and `cleanup` commands and the old filesystem-v1 layout (`{uuid}/chunk_00000` with age-writer chunks, version `1.0`). There is no migration path. Backups made with earlier versions cannot be read by this version. The new `upload` command is a separate format (`file-2`) using the repository chunk envelope.

### Fixed

- The rclone config path is no longer hardcoded. An empty ConfigPath lets rclone use its own default.
