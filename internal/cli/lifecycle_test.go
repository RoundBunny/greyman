package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"greyman/internal/config"
	"greyman/internal/settings"
	"greyman/internal/state"
)

const fakeDockerScript = `#!/bin/sh
echo "$*" >> "$FAKE_DOCKER_DIR/log"
running=false; [ -f "$FAKE_DOCKER_DIR/running" ] && running=true
case "$1" in
  inspect) $running && { echo true; exit 0; }; echo "Error: No such object" >&2; exit 1 ;;
  ps) [ -n "$FAKE_DOCKER_PS_FAIL" ] && exit 1; $running && echo abc123; exit 0 ;;
  run) env | grep -E '^(GREYMAN_|TZ=)' | sort > "$FAKE_DOCKER_DIR/env"; echo abc123 ;;
  port) echo 127.0.0.1:1 ;;
  logs) echo "boom: chromium crashed" ;;
esac
exit 0
`

// fakeDocker puts a stub `docker` first on PATH. The container never runs unless the
// test creates <dir>/running. It returns a func that reads the recorded docker calls.
func fakeDocker(t *testing.T) (dir string, calls func() []string) {
	t.Helper()
	dir = t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("FAKE_DOCKER_DIR", dir)
	t.Setenv("FAKE_DOCKER_PS_FAIL", "")
	return dir, func() []string {
		b, _ := os.ReadFile(filepath.Join(dir, "log"))
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}

func setup(t *testing.T) {
	t.Helper()
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	for _, s := range settings.All { // a developer's own exports must not leak into tests
		t.Setenv("GREYMAN_"+s.Key, "")
	}
	markInstalled(t)
}

func indexOf(calls []string, prefix string, from int) int {
	for i := from; i < len(calls); i++ {
		if strings.HasPrefix(calls[i], prefix) {
			return i
		}
	}
	return -1
}

func TestUpTearsDownAContainerThatFailsToStart(t *testing.T) {
	setup(t)
	_, calls := fakeDocker(t)
	code, _, errs := run("up")
	if code != 1 || !strings.Contains(errs, "container exited during startup") || !strings.Contains(errs, "boom: chromium crashed") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	c := calls()
	ran := indexOf(c, "run ", 0)
	if ran < 0 || indexOf(c, "rm -f greyman", ran) < 0 {
		t.Errorf("container was not removed after it failed to start: %q", c)
	}
	if _, err := state.Load(); err == nil {
		t.Error("state file left behind")
	}
}

func TestUpRefusesToKillARunningContainerItHasNoStateFor(t *testing.T) {
	setup(t)
	dir, calls := fakeDocker(t)
	if err := os.WriteFile(filepath.Join(dir, "running"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := run("up")
	if code != 1 || !strings.Contains(errs, "greyman down") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	for _, prefix := range []string{"stop", "rm", "run"} {
		if indexOf(calls(), prefix, 0) >= 0 {
			t.Errorf("up ran `docker %s…`: %q", prefix, calls())
		}
	}
}

func TestUpRejectsProxyCredentialsOnTheCommandLine(t *testing.T) {
	setup(t)
	fakeDocker(t)
	code, _, errs := run("up", "--proxy", "http://user:pw@proxy.test:3128")
	if code != 1 || !strings.Contains(errs, "GREYMAN_PROXY") || strings.Contains(errs, "pw") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestStaleStateSaysNotRunning(t *testing.T) {
	setup(t)
	fakeDocker(t)
	if err := state.Save(&state.State{Container: "greyman", Port: 1, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"snap"}, {"open", "example.com"}, {"status"}} {
		code, _, errs := run(args...)
		if code != 1 || !strings.Contains(errs, "greyman is not running; run `greyman up`") {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
}

func TestUnreachableDaemonKeepsItsErrorWhenDockerCannotAnswer(t *testing.T) {
	setup(t)
	fakeDocker(t)
	t.Setenv("FAKE_DOCKER_PS_FAIL", "1") // e.g. an agent sandbox that blocks the Docker socket
	if err := state.Save(&state.State{Container: "greyman", Port: 1, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	code, _, errs := run("snap")
	if code != 1 || !strings.Contains(errs, "daemon unreachable") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestUnreachableDaemonKeepsItsErrorWhileTheContainerRuns(t *testing.T) {
	setup(t)
	dir, _ := fakeDocker(t)
	if err := os.WriteFile(filepath.Join(dir, "running"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.State{Container: "greyman", Port: 1, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run("snap"); code != 1 || !strings.Contains(errs, "daemon unreachable") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestBadFlagExitsWithUsageCodeAndPrintsOnce(t *testing.T) {
	setup(t)
	for _, args := range [][]string{{"snap", "--bogus"}, {"scroll", "down", "-1"}} {
		code, _, errs := run(args...)
		if code != 2 || strings.Count(errs, "flag provided but not defined") != 1 || strings.Contains(errs, "error:") {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
	if code, _, _ := run("snap", "-h"); code != 0 {
		t.Errorf("-h: code=%d", code)
	}
}

func TestShotFullWithARefIsAUsageError(t *testing.T) {
	setup(t)
	if code, _, errs := run("shot", "e1", "--full"); code != 2 || !strings.Contains(errs, "usage: greyman shot") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestCtrlCExitsLikeAShell(t *testing.T) {
	setup(t)
	gotRequest := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(gotRequest)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	if err := state.Save(&state.State{Container: "greyman", Port: port, Token: "t"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-gotRequest; cancel() }()
	var out, errb bytes.Buffer
	if code := Run(ctx, []string{"type", "e1", "hello"}, &out, &errb); code != 130 || errb.String() != "interrupted\n" {
		t.Fatalf("code=%d err=%q", code, errb.String())
	}
}

// upAndGetRun runs `greyman up` against the fake docker, whose container never answers,
// so up fails after `docker run`. It returns that call and the GREYMAN_ environment the
// docker process saw.
func upAndGetRun(t *testing.T, args ...string) (call, env string) {
	t.Helper()
	dir, calls := fakeDocker(t)
	run(append([]string{"up"}, args...)...)
	c := calls()
	i := indexOf(c, "run ", 0)
	if i < 0 {
		t.Fatalf("docker run was not called: %q", c)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "env"))
	return c[i], string(b)
}

func setConfig(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := config.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpAppliesOptionsCfg(t *testing.T) {
	setup(t)
	setConfig(t, map[string]string{
		"NAV_GAP": "1000-2000", "PROXY": "http://u:pw@proxy.test:3128",
		"WATCH": "1", "API_PORT": "9000", "WATCH_PORT": "9001",
	})
	call, env := upAndGetRun(t)
	for _, want := range []string{"127.0.0.1:9000:7777", "127.0.0.1:9001:6080", "-e GREYMAN_NAV_GAP", "-e GREYMAN_PROXY"} {
		if !strings.Contains(call, want) {
			t.Errorf("docker run missing %q: %s", want, call)
		}
	}
	if strings.Contains(call, "pw@") || strings.Contains(call, "1000-2000") {
		t.Errorf("value leaked into docker argv: %s", call)
	}
	for _, want := range []string{"GREYMAN_NAV_GAP=1000-2000", "GREYMAN_PROXY=http://u:pw@proxy.test:3128", "GREYMAN_WATCH=1"} {
		if !strings.Contains(env, want) {
			t.Errorf("container env missing %q:\n%s", want, env)
		}
	}
	// setup blanks every GREYMAN_<KEY>, so look for the values, not the names.
	if strings.Contains(env, "9000") || strings.Contains(env, "9001") {
		t.Errorf("host-only setting reached the container:\n%s", env)
	}
}

func TestUpPrecedenceFlagThenEnvThenFile(t *testing.T) {
	setup(t)
	setConfig(t, map[string]string{"NAV_GAP": "1000-2000", "ACT_GAP": "10-20", "PROXY": "http://file.test:1", "HEADLESS": "1"})
	t.Setenv("GREYMAN_NAV_GAP", "7-8")
	_, env := upAndGetRun(t, "--proxy", "http://flag.test:2")
	for _, want := range []string{"GREYMAN_NAV_GAP=7-8", "GREYMAN_ACT_GAP=10-20", "GREYMAN_PROXY=http://flag.test:2", "GREYMAN_HEADLESS=1"} {
		if !strings.Contains(env, want) {
			t.Errorf("container env missing %q:\n%s", want, env)
		}
	}
	// An explicit false flag beats the file; a flag left alone does not.
	_, env = upAndGetRun(t, "--headless=false")
	if !strings.Contains(env, "GREYMAN_HEADLESS=0") {
		t.Errorf("--headless=false did not override the file:\n%s", env)
	}
}

func TestUpDefaultsToHeaded(t *testing.T) {
	setup(t)
	for _, args := range [][]string{nil, {"--watch"}} {
		if _, env := upAndGetRun(t, args...); !strings.Contains(env, "GREYMAN_HEADLESS=0") {
			t.Errorf("up %v is not headed:\n%s", args, env)
		}
	}
	if _, env := upAndGetRun(t, "--headless"); !strings.Contains(env, "GREYMAN_HEADLESS=1") {
		t.Errorf("up --headless is not headless:\n%s", env)
	}
}

func TestUpRejectsBadSettingsBeforeTouchingDocker(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]string
		args []string
		want string
	}{
		{"bad value", map[string]string{"NAV_GAP": "oops"}, nil, "NAV_GAP"},
		{"watch with headless from file", map[string]string{"HEADLESS": "1"}, []string{"--watch"}, "watch needs headed mode"},
		{"same port twice", map[string]string{"WATCH": "1", "API_PORT": "9000", "WATCH_PORT": "9000"}, nil, "must differ"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setup(t)
			_, calls := fakeDocker(t)
			setConfig(t, c.cfg)
			code, _, errs := run(append([]string{"up"}, c.args...)...)
			if code != 1 || !strings.Contains(errs, c.want) {
				t.Fatalf("code=%d err=%q, want %q", code, errs, c.want)
			}
			if got := calls(); len(got) != 1 || got[0] != "" {
				t.Errorf("docker was called: %q", got)
			}
		})
	}
}

func TestUpWarnsAboutAnInvalidHostTZButStillStarts(t *testing.T) {
	setup(t)
	t.Setenv("TZ", ":America/Chicago") // host detection takes $TZ as is
	_, calls := fakeDocker(t)
	_, _, errs := run("up")
	if !strings.Contains(errs, "warning: detected host TZ") {
		t.Errorf("want a TZ warning, got %q", errs)
	}
	if indexOf(calls(), "run ", 0) < 0 {
		t.Error("up must go ahead despite the warning")
	}
}
