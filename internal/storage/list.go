package storage

import "encoding/json"

// ListEntry is the subset of rclone lsjson output that gloak uses.
type ListEntry struct {
	Path  string `json:"Path"`
	IsDir bool   `json:"IsDir"`
}

func ParseList(raw []byte) ([]ListEntry, error) {
	var entries []ListEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
