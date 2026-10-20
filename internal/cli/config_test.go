package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"greyman/internal/config"
	"greyman/internal/state"
)

func cfgFile(t *testing.T) string {
	t.Helper()
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "options.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var spaces = regexp.MustCompile(` +`)

func TestConfigListShowsValuesAndWhereTheyCameFrom(t *testing.T) {
	setup(t)
	setConfig(t, map[string]string{"NAV_GAP": "1000-2000", "MAX_CHARS": "oops"})
	t.Setenv("GREYMAN_ACT_GAP", "5-6")
	for _, args := range [][]string{{"config"}, {"config", "list"}} {
		code, out, errs := run(args...)
		if code != 0 {
			t.Fatalf("%v: code=%d err=%q", args, code, errs)
		}
		out = spaces.ReplaceAllString(out, " ") // columns are padded
		for _, want := range []string{
			"NAV_GAP=1000-2000 [file] ",
			"ACT_GAP=5-6 [env] ",
			"HEADLESS=0 [default] ",
			"MAX_CHARS=oops [file] INVALID: want a positive integer",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: output missing %q:\n%s", args, want, out)
			}
		}
	}
}

func TestConfigSetGetUnset(t *testing.T) {
	setup(t)
	if code, out, errs := run("config", "set", "NAV_GAP", "1000-2000"); code != 0 || out != "ok NAV_GAP=1000-2000\n" {
		t.Fatalf("set: code=%d out=%q err=%q", code, out, errs)
	}
	if !strings.Contains(cfgFile(t), "\nNAV_GAP=1000-2000\n") {
		t.Fatalf("file = %q", cfgFile(t))
	}
	if code, out, _ := run("config", "get", "NAV_GAP"); code != 0 || out != "1000-2000\n" {
		t.Fatalf("get: code=%d out=%q", code, out)
	}
	if code, out, errs := run("config", "unset", "NAV_GAP"); code != 0 || out != "ok unset NAV_GAP\n" {
		t.Fatalf("unset: code=%d out=%q err=%q", code, out, errs)
	}
	if code, out, _ := run("config", "get", "NAV_GAP"); code != 0 || out != "3000-6000\n" {
		t.Fatalf("get after unset: code=%d out=%q", code, out)
	}
	if strings.Contains(cfgFile(t), "NAV_GAP") || !strings.Contains(cfgFile(t), "INSTALLED_AGENTS=other") {
		t.Fatalf("file = %q", cfgFile(t))
	}
}

func TestConfigRejectsBadInputWithoutWritingIt(t *testing.T) {
	setup(t)
	before := cfgFile(t)
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"set", "NAV_GAP", "9-1"}, 1, "invalid NAV_GAP"},
		{[]string{"set", "NOPE", "1"}, 1, `unknown setting "NOPE"`},
		{[]string{"set", "INSTALLED_AGENTS", "x"}, 1, "unknown setting"},
		{[]string{"set", "NAV_GAP", ""}, 1, "greyman config unset NAV_GAP"},
		{[]string{"set", "PROXY", "http://u:pw@proxy:1"}, 1, "options.cfg"},
		{[]string{"get", "NOPE"}, 1, "unknown setting"},
		{[]string{"unset", "NOPE"}, 1, "unknown setting"},
		{[]string{"set", "NAV_GAP"}, 2, "usage: greyman config"},
		{[]string{"get"}, 2, "usage: greyman config"},
		{[]string{"bogus"}, 2, "usage: greyman config"},
		{[]string{"list", "extra"}, 2, "usage: greyman config"},
	}
	for _, c := range cases {
		code, _, errs := run(append([]string{"config"}, c.args...)...)
		if code != c.code || !strings.Contains(errs, c.want) {
			t.Errorf("%v: code=%d err=%q, want code %d and %q", c.args, code, errs, c.code, c.want)
		}
		if strings.Contains(errs, "pw") {
			t.Errorf("%v: error echoes the credential: %q", c.args, errs)
		}
	}
	if got := cfgFile(t); got != before {
		t.Errorf("file changed: %q -> %q", before, got)
	}
}

func TestConfigNeverPrintsAProxyPassword(t *testing.T) {
	setup(t)
	setConfig(t, map[string]string{"PROXY": "http://user:hunter2@proxy.test:3128"})
	for _, args := range [][]string{{"config"}, {"config", "get", "PROXY"}} {
		code, out, errs := run(args...)
		if code != 0 || strings.Contains(out+errs, "hunter2") || !strings.Contains(out, "http://user:***@proxy.test:3128") {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
	}
	if code, out, _ := run("config", "set", "PROXY", "proxy.test:3128"); code != 0 || out != "ok PROXY=proxy.test:3128\n" {
		t.Errorf("set without credentials: code=%d out=%q", code, out)
	}
}

func TestConfigGetFailsOnAnInvalidValue(t *testing.T) {
	setup(t)
	setConfig(t, map[string]string{"NAV_GAP": "oops"})
	if code, _, errs := run("config", "get", "NAV_GAP"); code != 1 || !strings.Contains(errs, "invalid NAV_GAP (options.cfg)") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestConfigWorksBeforeInstall(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir()) // no INSTALLED_AGENTS
	if code, out, errs := run("config", "set", "MAX_CHARS", "500"); code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if code, _, errs := run("snap"); code != 1 || !strings.Contains(errs, "greyman install") {
		t.Fatalf("snap should still be gated: code=%d err=%q", code, errs)
	}
}

func TestConfigNotesWhenTheContainerIsAlreadyRunning(t *testing.T) {
	setup(t)
	dir, _ := fakeDocker(t)
	if _, out, _ := run("config", "set", "MAX_CHARS", "500"); strings.Contains(out, "note:") {
		t.Fatalf("note printed with no container: %q", out)
	}
	if err := os.WriteFile(filepath.Join(dir, "running"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.State{Container: "greyman", Port: 1, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"set", "MAX_CHARS", "600"}, {"unset", "MAX_CHARS"}} {
		if _, out, _ := run(append([]string{"config"}, args...)...); !strings.Contains(out, "note: the container is running") {
			t.Errorf("%v: no note in %q", args, out)
		}
	}
}
