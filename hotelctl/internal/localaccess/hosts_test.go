package localaccess

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestHostsInstallRepeatRemovePreservesUserEntries(t *testing.T) {
	original := []byte("127.0.0.1 localhost\n::1 localhost\n192.0.2.1 personal-host # keep\n")
	installed, err := MergeHosts(original, false)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := MergeHosts(installed, false)
	if err != nil || !bytes.Equal(installed, repeated) {
		t.Fatalf("install is not idempotent: %v", err)
	}
	removed, err := MergeHosts(installed, true)
	if err != nil || !bytes.Equal(original, removed) {
		t.Fatalf("user entries changed: %q, %v", removed, err)
	}
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateHosts(path, false); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, installed) {
		t.Fatalf("file update failed: %v", err)
	}
}

func TestHostsRejectsConflictsAndMalformedBlocks(t *testing.T) {
	for _, input := range []string{
		"192.0.2.1 booking-postgres\n",
		"::1 CHECK-IN-POSTGRES\n",
		beginMarker + "\n",
		endMarker + "\n" + beginMarker,
		HostsBlock() + HostsBlock(),
	} {
		if _, err := MergeHosts([]byte(input), false); err == nil {
			t.Fatalf("accepted conflicting input: %q", input)
		}
	}
}
