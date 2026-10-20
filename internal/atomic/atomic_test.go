package atomic

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteFileCreatesAndReplaces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	for _, content := range []string{"one", "two"} {
		if err := WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(p); err != nil || string(b) != content {
			t.Fatalf("file = %q, %v; want %q", b, err, content)
		}
	}
}

func TestWriteFileSetsModeAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	for _, perm := range []os.FileMode{0o600, 0o644} {
		p := filepath.Join(dir, "f")
		if err := WriteFile(p, []byte("x"), perm); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != perm {
			t.Fatalf("mode = %v, %v; want %v", fi.Mode().Perm(), err, perm)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("directory has %d entries, want just the file", len(entries))
	}
}

func TestWriteFileFailureLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	// The target is a non-empty directory, so the final rename fails.
	target := filepath.Join(dir, "f")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(target, []byte("x"), 0o600); err == nil {
		t.Fatal("want an error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("directory has %d entries, want just the original", len(entries))
	}
}

func TestWriteFileConcurrentWritersDoNotCollide(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := WriteFile(p, []byte("data"), 0o600); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if b, err := os.ReadFile(p); err != nil || string(b) != "data" {
		t.Fatalf("file = %q, %v", b, err)
	}
}
