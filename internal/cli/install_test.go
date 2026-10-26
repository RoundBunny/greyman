package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"greyman/internal/agents"
	"greyman/internal/config"
	"greyman/internal/version"
)

// installEnv isolates install from the real machine: a temp agent home, no agents on
// PATH, and a temp HOME for ~/.greyman.
func installEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	apps := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	t.Setenv("COPILOT_HOME", "")
	old := hostFunc
	hostFunc = func() (agents.Host, error) { return agents.Host{Home: home, AppsDir: apps}, nil }
	t.Cleanup(func() { hostFunc = old })
	return home
}

func installed(t *testing.T) []string {
	t.Helper()
	names, err := installedAgents()
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func readString(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallOneAgent(t *testing.T) {
	home := installEnv(t)
	tests := []struct {
		name  string
		files []string // relative to home
		line  string
	}{
		{"claude", []string{".claude/skills/greyman/SKILL.md"}, "ok claude skill=" + home + "/.claude/skills/greyman/SKILL.md\n"},
		{"codex", []string{".agents/skills/greyman/SKILL.md", ".codex/rules/greyman.rules"},
			"ok codex skill=" + home + "/.agents/skills/greyman/SKILL.md rules=" + home + "/.codex/rules/greyman.rules\n"},
		{"cursor", []string{".agents/skills/greyman/SKILL.md"}, "ok cursor skill=" + home + "/.agents/skills/greyman/SKILL.md\n"},
		{"copilot", []string{".agents/skills/greyman/SKILL.md"}, "ok copilot skill=" + home + "/.agents/skills/greyman/SKILL.md\n"},
	}
	for _, tt := range tests {
		code, out, errs := run("install", tt.name)
		if code != 0 || !strings.HasPrefix(out, tt.line) {
			t.Errorf("%s: code=%d out=%q err=%q", tt.name, code, out, errs)
		}
		for _, f := range tt.files {
			if _, err := os.Stat(filepath.Join(home, f)); err != nil {
				t.Errorf("%s: %v", tt.name, err)
			}
		}
	}
	skill := readString(t, home, ".agents/skills/greyman/SKILL.md")
	if !strings.HasPrefix(skill, "---\nname: greyman\ndescription: ") {
		t.Errorf("SKILL.md frontmatter: %.80q", skill)
	}
	if got := installed(t); !slices.Equal(got, []string{"claude", "codex", "copilot", "cursor"}) {
		t.Errorf("INSTALLED_AGENTS = %v", got)
	}
	if v, _ := config.Get(keyVersion); v != version.Version {
		t.Errorf("INSTALLED_VERSION = %q, want %q", v, version.Version)
	}
}

func TestInstallAllSharesOneSkillFile(t *testing.T) {
	home := installEnv(t)
	code, out, errs := run("install", "all")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	shared := home + "/.agents/skills/greyman/SKILL.md"
	if n := strings.Count(out, "skill="+shared); n != 3 {
		t.Errorf("shared skill path appears %d times, want 3 (codex, cursor, copilot):\n%s", n, out)
	}
	if !strings.HasSuffix(out, "ok installed=claude,codex,copilot,cursor\n") {
		t.Errorf("summary missing:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, "other")); err == nil {
		t.Error("all must not include other")
	}
}

func TestInstallIsIdempotentAndUnionsConfig(t *testing.T) {
	home := installEnv(t)
	for _, name := range []string{"claude", "codex", "claude"} {
		if code, _, errs := run("install", name); code != 0 {
			t.Fatalf("install %s: code=%d err=%q", name, code, errs)
		}
	}
	if got := installed(t); !slices.Equal(got, []string{"claude", "codex"}) {
		t.Errorf("INSTALLED_AGENTS = %v", got)
	}
	// Stale content is refreshed.
	p := filepath.Join(home, ".claude/skills/greyman/SKILL.md")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run("install", "claude"); code != 0 || readString(t, p) == "old" {
		t.Error("skill file was not refreshed")
	}
}

func TestInstallDetectsAgents(t *testing.T) {
	home := installEnv(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errs := run("install")
	if code != 0 || !strings.HasSuffix(out, "ok installed=claude,codex\n") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/greyman/SKILL.md")); err != nil {
		t.Error("codex skill missing")
	}
}

func TestInstallNothingDetected(t *testing.T) {
	home := installEnv(t)
	code, out, errs := run("install")
	if code != 1 || out != "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	for _, want := range []string{"claude, codex, cursor, copilot", "greyman install all", "greyman install other"} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr missing %q: %q", want, errs)
		}
	}
	if n := installed(t); len(n) != 0 {
		t.Errorf("recorded %v", n)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("wrote into home: %v", entries)
	}
}

func TestInstallUsageErrors(t *testing.T) {
	installEnv(t)
	for _, args := range [][]string{
		{"install", "bogus"},
		{"install", "cursur"},
		{"install", "claude", "codex"},
		{"install", "claude", "/some/dir"},
		{"install", "all", "/some/dir"},
		{"install", "other", "a", "b"},
	} {
		if code, _, errs := run(args...); code != 2 {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
	if code, _, errs := run("install", "bogus"); !strings.Contains(errs, `unknown agent "bogus"`) || code != 2 {
		t.Errorf("unknown agent message: %q", errs)
	}
	if n := installed(t); len(n) != 0 {
		t.Errorf("usage errors recorded %v", n)
	}
}

func TestInstallOther(t *testing.T) {
	installEnv(t)
	cwd := t.TempDir()
	t.Chdir(cwd)

	code, out, errs := run("install", "other")
	if code != 0 || !strings.HasPrefix(out, "ok other skill=SKILL.md requirements=REQUIREMENTS.md\n") {
		t.Fatalf("default dir: code=%d out=%q err=%q", code, out, errs)
	}
	if !strings.HasPrefix(readString(t, cwd, "SKILL.md"), "---\nname: greyman\n") {
		t.Error("SKILL.md content")
	}
	if !strings.Contains(readString(t, cwd, "REQUIREMENTS.md"), "Docker daemon socket") {
		t.Error("REQUIREMENTS.md content")
	}
	if got := installed(t); !slices.Equal(got, []string{"other"}) {
		t.Errorf("INSTALLED_AGENTS = %v", got)
	}

	dir := filepath.Join(cwd, "new", "place")
	for _, f := range []string{"SKILL.md", "REQUIREMENTS.md"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if code, _, errs := run("install", "other", dir); code != 0 {
		t.Fatalf("custom dir: code=%d err=%q", code, errs)
	}
	for _, f := range []string{"SKILL.md", "REQUIREMENTS.md"} {
		if readString(t, dir, f) == "stale" {
			t.Errorf("%s not rewritten", f)
		}
	}
}

func TestCodexRulesAreKeptUnlessForced(t *testing.T) {
	home := installEnv(t)
	rules := filepath.Join(home, ".codex", "rules", "greyman.rules")
	if code, _, errs := run("install", "codex"); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	ours := readString(t, rules)
	for _, want := range []string{`pattern = ["greyman"],`, `decision = "allow"`} {
		if !strings.Contains(ours, want) {
			t.Errorf("rules missing %q", want)
		}
	}
	if strings.Contains(ours, "prompt") || strings.Count(ours, "prefix_rule(") != 1 {
		t.Errorf("rules should be a single allow-all rule:\n%s", ours)
	}

	if code, out, _ := run("install", "codex"); code != 0 || strings.Contains(out, "kept") {
		t.Errorf("identical rules should not report kept: %q", out)
	}

	if err := os.WriteFile(rules, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run("install", "codex")
	if code != 0 || !strings.Contains(out, "kept existing rules="+rules) || readString(t, rules) != "# mine\n" {
		t.Errorf("edited rules: code=%d out=%q", code, out)
	}
	if code, out, _ := run("install", "codex", "--force"); code != 0 || strings.Contains(out, "kept") || readString(t, rules) != ours {
		t.Errorf("--force: code=%d out=%q", code, out)
	}
}

func TestCodexHomeOverridesRulesDir(t *testing.T) {
	home := installEnv(t)
	ch := filepath.Join(t.TempDir(), "ch")
	t.Setenv("CODEX_HOME", ch)
	if code, _, errs := run("install", "codex"); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	if _, err := os.Stat(filepath.Join(ch, "rules", "greyman.rules")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); err == nil {
		t.Error("wrote to ~/.codex despite CODEX_HOME")
	}
}

func TestFailedAgentIsNotRecorded(t *testing.T) {
	home := installEnv(t)
	// ~/.claude is a file, so its skills directory cannot be created.
	if err := os.WriteFile(filepath.Join(home, ".claude"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := run("install", "all")
	if code != 1 || !strings.Contains(errs, "fail claude skill=") || strings.Contains(out, "ok claude") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if got := installed(t); !slices.Equal(got, []string{"codex", "copilot", "cursor"}) {
		t.Errorf("INSTALLED_AGENTS = %v", got)
	}
}

func TestGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{{"snap"}, {"up"}, {"status"}, {"click", "e1"}, {"shot"}} {
		code, _, errs := run(args...)
		if code != 1 || !strings.Contains(errs, "greyman is not set up for any agent; run `greyman install`") {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
	if code, _, _ := run("help"); code != 0 {
		t.Errorf("help gated: code=%d", code)
	}

	// An empty value does not satisfy the gate; any recorded agent does.
	if err := config.Set(keyAgents, ""); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run("snap"); code != 1 || !strings.Contains(errs, "not set up") {
		t.Errorf("empty value: code=%d err=%q", code, errs)
	}
	markInstalled(t)
	if _, _, errs := run("snap"); strings.Contains(errs, "not set up") {
		t.Errorf("gate still closed: %q", errs)
	}
}

// New commands must be gated unless someone exempts them on purpose.
func TestOnlyConfigInstallDownAndVersionAreUngated(t *testing.T) {
	var got []string
	for _, c := range commands() {
		if ungated[c.name] {
			got = append(got, c.name)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"config", "down", "install", "version"}) {
		t.Fatalf("ungated commands = %v", got)
	}
}
