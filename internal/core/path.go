package core

import (
	"fmt"
	"strings"
)

// ValidateRelPath rejects tree entry paths that could escape the restore
// root. The manifest is remote input; this is the trust boundary.
func ValidateRelPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("empty path")
	}
	if strings.ContainsRune(rel, 0) {
		return fmt.Errorf("path contains null byte")
	}
	if strings.Contains(rel, `\`) {
		return fmt.Errorf("path contains backslash: %s", rel)
	}
	if strings.HasPrefix(rel, "/") {
		return fmt.Errorf("absolute path: %s", rel)
	}
	if len(rel) >= 2 && rel[1] == ':' {
		return fmt.Errorf("drive letter path: %s", rel)
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("unsafe path segment in %s", rel)
		}
	}
	return nil
}
