// Package config loads the optional user configuration file.
//
// The file lives at <gloak-dir>/config.toml (GLOAK_DIR or ~/.gloak). A
// missing file is not an error; every value stays empty and callers fall
// back to their defaults.
package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Telegram holds Bot API credentials for optional notifications.
type Telegram struct {
	Token  string `toml:"token"`
	ChatID string `toml:"chat_id"`
}

// Config is the user configuration file.
type Config struct {
	Telegram Telegram `toml:"telegram"`
}

// Path returns the config file location for the given gloak directory.
func Path(dir string) string {
	return filepath.Join(dir, "config.toml")
}

// Load reads path and parses it. A missing file yields an empty Config and
// a nil error. Other read errors and TOML parsing errors are returned with an
// empty Config.
func Load(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
