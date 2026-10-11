package repo

const (
	FormatVersion   = "2.0"
	ConfigPath      = "repo.json"
	KeysPrefix      = "keys"
	SnapshotsPrefix = "snapshots"
	DataPrefix      = "data"
)

// Config is the plaintext repository descriptor. It carries no user data:
// version, creation time, recipient public keys, and chunk parameters.
type Config struct {
	Version    string   `json:"version"`
	CreatedAt  string   `json:"created_at"`
	Recipients []string `json:"recipients"`
	ChunkSize  int64    `json:"chunk_size"`
}
