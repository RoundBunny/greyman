package agents

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// isolate returns a Host over empty temp dirs, with PATH and the agent env vars cleared.
func isolate(t *testing.T) Host {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	t.Setenv("COPILOT_HOME", "")
	return Host{Home: t.TempDir(), AppsDir: t.TempDir()}
}

func mkdir(t *testing.T, parts ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(parts...), 0o755); err != nil {
		t.Fatal(err)
	}
}

func names(as []Agent) []string {
	var n []string
	for _, a := range as {
		n = append(n, a.Name)
	}
	return n
}

func TestDetected(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, h Host)
		want  []string
	}{
		{"nothing", func(*testing.T, Host) {}, nil},
		{"claude dir", func(t *testing.T, h Host) { mkdir(t, h.Home, ".claude") }, []string{"claude"}},
		{"codex dir", func(t *testing.T, h Host) { mkdir(t, h.Home, ".codex") }, []string{"codex"}},
		{"codex home", func(t *testing.T, h Host) {
			d := filepath.Join(t.TempDir(), "elsewhere")
			mkdir(t, d)
			t.Setenv("CODEX_HOME", d)
		}, []string{"codex"}},
		{"copilot dir", func(t *testing.T, h Host) { mkdir(t, h.Home, ".copilot") }, []string{"copilot"}},
		{"copilot vscode extension", func(t *testing.T, h Host) {
			mkdir(t, h.Home, ".vscode", "extensions", "github.copilot-1.2.3")
		}, []string{"copilot"}},
		{"cursor app", func(t *testing.T, h Host) { mkdir(t, h.AppsDir, "Cursor.app") }, []string{"cursor"}},
		{"cursor dir", func(t *testing.T, h Host) { mkdir(t, h.Home, ".cursor") }, []string{"cursor"}},
		{"on PATH", func(t *testing.T, h Host) {
			dir := t.TempDir()
			for _, bin := range []string{"claude", "cursor-agent"} {
				if err := os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir)
		}, []string{"claude", "cursor"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := isolate(t)
			tt.setup(t, h)
			if got := names(Detected(h)); !slices.Equal(got, tt.want) {
				t.Fatalf("Detected = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOtherIsNeverDetectedOrKnown(t *testing.T) {
	if _, ok := Lookup("other"); ok {
		t.Fatal("other must not be in Known")
	}
}

func TestCodexTargets(t *testing.T) {
	h := isolate(t)
	ts := codexTargets(h, "")
	if len(ts) != 2 || ts[0].Label != "skill" || ts[1].Label != "rules" || !ts[1].Preserve {
		t.Fatalf("targets = %+v", ts)
	}
	if want := filepath.Join(h.Home, ".codex", "rules", "greyman.rules"); ts[1].Path != want {
		t.Fatalf("rules path = %s, want %s", ts[1].Path, want)
	}
	t.Setenv("CODEX_HOME", "/custom")
	if got := codexTargets(h, "")[1].Path; got != "/custom/rules/greyman.rules" {
		t.Fatalf("rules path with CODEX_HOME = %s", got)
	}
}

func TestWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "f")
	t.Run("plain files are always rewritten", func(t *testing.T) {
		tg := Target{Path: p, Data: []byte("one")}
		if _, err := Write(tg, false); err != nil {
			t.Fatal(err)
		}
		tg.Data = []byte("two")
		if kept, err := Write(tg, false); err != nil || kept {
			t.Fatalf("kept=%v err=%v", kept, err)
		}
		if b, _ := os.ReadFile(p); string(b) != "two" {
			t.Fatalf("file = %q", b)
		}
	})
	t.Run("preserved file", func(t *testing.T) {
		tg := Target{Path: filepath.Join(t.TempDir(), "r"), Data: []byte("ours"), Preserve: true}
		if kept, err := Write(tg, false); err != nil || kept {
			t.Fatalf("first write: kept=%v err=%v", kept, err)
		}
		if kept, err := Write(tg, false); err != nil || kept {
			t.Fatalf("identical file should not count as kept: kept=%v err=%v", kept, err)
		}
		if err := os.WriteFile(tg.Path, []byte("edited"), 0o644); err != nil {
			t.Fatal(err)
		}
		if kept, err := Write(tg, false); err != nil || !kept {
			t.Fatalf("edited file: kept=%v err=%v", kept, err)
		}
		if b, _ := os.ReadFile(tg.Path); string(b) != "edited" {
			t.Fatalf("edited file was overwritten: %q", b)
		}
		if kept, err := Write(tg, true); err != nil || kept {
			t.Fatalf("force: kept=%v err=%v", kept, err)
		}
		if b, _ := os.ReadFile(tg.Path); string(b) != "ours" {
			t.Fatalf("force did not replace: %q", b)
		}
	})
}
