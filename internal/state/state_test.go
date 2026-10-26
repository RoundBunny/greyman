package state

import (
	"errors"
	"os"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := Load(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Load on empty dir = %v, want ErrNotRunning", err)
	}

	want := &State{Container: "greyman", Port: 4242, Token: "tok"}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || *got != *want {
		t.Fatalf("Load = %+v, %v; want %+v", got, err, want)
	}

	p, _ := path()
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}

	if err := Remove(); err != nil {
		t.Fatal(err)
	}
	if err := Remove(); err != nil {
		t.Fatalf("second Remove should be a no-op, got %v", err)
	}
	if _, err := Load(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Load after Remove = %v, want ErrNotRunning", err)
	}
}
