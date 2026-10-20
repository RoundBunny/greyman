// Package agents knows where each coding agent keeps its skills, how to detect
// it, and writes greyman's skill files there.
package agents

import (
	"bytes"
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"greyman/internal/atomic"
)

//go:embed assets/SKILL.md
var skill []byte

//go:embed assets/REQUIREMENTS.md
var requirements []byte

//go:embed assets/greyman.rules
var rules []byte

// Host is the machine that detection and target paths depend on. Tests build one
// over a temp directory.
type Host struct {
	Home    string // user home directory
	AppsDir string // where macOS apps live
}

func DefaultHost() (Host, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Host{}, err
	}
	return Host{Home: home, AppsDir: "/Applications"}, nil
}

// Target is one file to write.
type Target struct {
	Label    string // "skill", "rules" or "requirements"; shown in install output
	Path     string
	Data     []byte
	Preserve bool // user-tunable: keep an existing, different file unless forced
}

type Agent struct {
	Name    string
	Detect  func(Host) bool
	Targets func(h Host, dir string) []Target // dir is only used by Other
}

// Known are the agents greyman installs by name, in the order `all` and detection use.
var Known = []Agent{
	{"claude", detectClaude, claudeTargets},
	{"codex", detectCodex, codexTargets},
	{"cursor", detectCursor, sharedTargets},
	{"copilot", detectCopilot, sharedTargets},
}

// Other writes SKILL.md and REQUIREMENTS.md into a directory, for any agent not in
// Known. It is never detected.
var Other = Agent{Name: "other", Targets: otherTargets}

// Lookup finds a known agent by name.
func Lookup(name string) (Agent, bool) {
	for _, a := range Known {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// Detected returns the known agents that appear to be installed on h.
func Detected(h Host) []Agent {
	var found []Agent
	for _, a := range Known {
		if a.Detect(h) {
			found = append(found, a)
		}
	}
	return found
}

// Write puts t on disk, creating parent directories. It reports kept=true when t is
// Preserve, force is off, and a different file already exists (nothing is written).
func Write(t Target, force bool) (kept bool, err error) {
	if t.Preserve && !force {
		old, err := os.ReadFile(t.Path)
		switch {
		case err == nil:
			return !bytes.Equal(old, t.Data), nil
		case !errors.Is(err, fs.ErrNotExist):
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
		return false, err
	}
	return false, atomic.WriteFile(t.Path, t.Data, 0o644)
}

func skillTarget(path string) Target { return Target{Label: "skill", Path: path, Data: skill} }

func claudeTargets(h Host, _ string) []Target {
	return []Target{skillTarget(filepath.Join(h.Home, ".claude", "skills", "greyman", "SKILL.md"))}
}

// sharedTargets is the ~/.agents/skills location Codex, Copilot and Cursor all read.
func sharedTargets(h Host, _ string) []Target {
	return []Target{skillTarget(filepath.Join(h.Home, ".agents", "skills", "greyman", "SKILL.md"))}
}

func codexTargets(h Host, dir string) []Target {
	rulesPath := filepath.Join(codexHome(h), "rules", "greyman.rules")
	return append(sharedTargets(h, dir), Target{Label: "rules", Path: rulesPath, Data: rules, Preserve: true})
}

func otherTargets(_ Host, dir string) []Target {
	return []Target{
		skillTarget(filepath.Join(dir, "SKILL.md")),
		{Label: "requirements", Path: filepath.Join(dir, "REQUIREMENTS.md"), Data: requirements},
	}
}

func codexHome(h Host) string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(h.Home, ".codex")
}

func detectClaude(h Host) bool {
	return onPath("claude") || exists(filepath.Join(h.Home, ".claude"))
}

func detectCodex(h Host) bool {
	return onPath("codex") || exists(filepath.Join(h.Home, ".codex"), os.Getenv("CODEX_HOME"))
}

func detectCursor(h Host) bool {
	return onPath("cursor", "cursor-agent") ||
		exists(filepath.Join(h.AppsDir, "Cursor.app"), filepath.Join(h.Home, ".cursor"))
}

// detectCopilot also looks for the VS Code extension, which has no CLI. That
// extension path is a guess, not documented.
func detectCopilot(h Host) bool {
	ext, _ := filepath.Glob(filepath.Join(h.Home, ".vscode", "extensions", "github.copilot-*"))
	return onPath("copilot") || exists(filepath.Join(h.Home, ".copilot"), os.Getenv("COPILOT_HOME")) || len(ext) > 0
}

func onPath(names ...string) bool {
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			return true
		}
	}
	return false
}

// exists reports whether any path exists; empty paths are skipped.
func exists(paths ...string) bool {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
