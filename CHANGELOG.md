# Changelog

## Unreleased

### Added

- `gloak init` creates a repository: a wrapped master key per recipient under `keys/`, a `repo.json` manifest, format 2.0.
- `gloak backup` uploads content-addressed encrypted chunks under `data/{hh}/`, records them in an encrypted epoch index under `index/`, and writes a snapshot under `snapshots/`. Repeat backups skip chunks already in the index. Chunks are sealed with zstd and then XChaCha20-Poly1305 under HKDF-derived keys.
- Snapshots are encrypted to every recipient listed at init time. Any matching identity can verify or restore.

### Changed

- `verify` and `restore` read the repository layout. Corruption errors keep the CHUNK CORRUPT prefix.

### Removed

- The `upload`, `resume`, and `cleanup` commands and the old filesystem-v1 single-file layout (`{uuid}/chunk_00000`, `manifest.age`, `locator.json`). There is no migration path. Backups made with earlier versions cannot be read by this version.

### Fixed

- The rclone config path is no longer hardcoded. An empty ConfigPath lets rclone use its own default.
