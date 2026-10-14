package project

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
)

func TestStoreLockAndOwnershipPersistence(t *testing.T) {
	store := Store{
		Dir: t.TempDir(),
	}
	unlock, err := store.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if second, err := store.Lock(); err == nil {
		second()
		t.Fatal("concurrent operation was allowed")
	}
	original, err := NewState(config.Config{
		Name: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	original.Images["gateway"] = "image:sha-123"
	if err := store.Save(original); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Owner != original.Owner || loaded.Images["gateway"] != "image:sha-123" {
		t.Fatal("state was not preserved")
	}
	info, err := os.Stat(filepath.Join(store.Dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("state permissions: %v", info.Mode())
	}
	unlock()
	unlock, err = store.Lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestMalformedStateIsNeverTreatedAsFreshInstall(t *testing.T) {
	store := Store{
		Dir: t.TempDir(),
	}
	for _, data := range []string{
		`{`,
		`{"version":1,"owner":"","name":"test"}`,
		`{"version":2,"owner":"owner","name":"test"}`,
	} {
		if err := os.WriteFile(filepath.Join(store.Dir, "state.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatalf("accepted invalid state %s", data)
		}
	}
}
