package main

import (
	"strings"
	"testing"
)

func TestInitPQFlagNotImplemented(t *testing.T) {
	cmd := InitCmd{Remote: "mem:", Recipient: "age1test", PQ: true}
	err := cmd.Run()
	if err == nil {
		t.Fatal("InitCmd with --pq succeeded")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveInitRecipientsAcceptsSSHEd25519(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@host"
	got, err := resolveInitRecipients(key)
	if err != nil {
		t.Fatalf("resolveInitRecipients() error: %v", err)
	}
	if len(got) != 1 || got[0] != key {
		t.Fatalf("got = %v", got)
	}
}
