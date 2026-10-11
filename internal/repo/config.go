package repo

const (
	FormatVersion   = "2.0"
	ConfigPath      = "repo.json"
	KeysPrefix      = "keys"
	SnapshotsPrefix = "snapshots"
	DataPrefix      = "data"

	DefaultChunkMin = 512 * 1024
	DefaultChunkAvg = 1024 * 1024
	DefaultChunkMax = 8 * 1024 * 1024
)

// Config is the plaintext repository descriptor. It carries no user data:
// version, creation time, recipient public keys, and CDC chunk parameters.
type Config struct {
	Version    string   `json:"version"`
	CreatedAt  string   `json:"created_at"`
	Recipients []string `json:"recipients"`
	ChunkMin   int64    `json:"chunk_min"`
	ChunkAvg   int64    `json:"chunk_avg"`
	ChunkMax   int64    `json:"chunk_max"`
}
