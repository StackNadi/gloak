package core

import "testing"

func TestValidateRelPathAcceptsNormal(t *testing.T) {
	for _, p := range []string{"a", "a/b", "a/b/c.txt", "dir/empty"} {
		if err := ValidateRelPath(p); err != nil {
			t.Fatalf("ValidateRelPath(%q) error: %v", p, err)
		}
	}
}

func TestValidateRelPathRejectsUnsafe(t *testing.T) {
	for _, p := range []string{"", "/abs", "a//b", "a/../b", "..", ".", "a/.", `a\b`, "a\x00b", "C:/x", "c:\\x"} {
		if err := ValidateRelPath(p); err == nil {
			t.Fatalf("ValidateRelPath(%q) succeeded", p)
		}
	}
}
