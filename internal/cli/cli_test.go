package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"greyman/internal/config"
	"greyman/internal/state"
	"greyman/internal/version"
)

type recorded struct {
	path string
	body map[string]any
}

// fakeDaemon points the CLI's saved state at an httptest server and records requests.
func fakeDaemon(t *testing.T, reply map[string]any) *[]recorded {
	t.Helper()
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, recorded{r.URL.Path, body})
		w.Header().Set("X-Greyman-Version", version.Version)
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	markInstalled(t)
	if err := state.Save(&state.State{Container: "greyman", Port: port, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	return &got
}

// markInstalled satisfies the setup gate in the current GREYMAN_CONFIG_DIR.
func markInstalled(t *testing.T) {
	t.Helper()
	if err := config.Set(keyAgents, "other"); err != nil {
		t.Fatal(err)
	}
}

func run(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestTypeAcceptsFlagsAfterPositionals(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok"})
	code, out, errs := run("type", "e3", "hello world", "--submit", "--tab", "t2")
	if code != 0 || strings.TrimSpace(out) != "ok" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	r := (*got)[0]
	if r.path != "/v1/type" || r.body["ref"] != "e3" || r.body["text"] != "hello world" ||
		r.body["submit"] != true || r.body["tab"] != "t2" {
		t.Fatalf("request = %+v", r)
	}
}

func TestDoubleDashMakesTextPositional(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok"})
	if code, _, errs := run("type", "e3", "--submit", "--", "-5"); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	if b := (*got)[0].body; b["text"] != "-5" || b["submit"] != true {
		t.Fatalf("body = %v", b)
	}
}

func TestSnapFlagAppendsSnapshot(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "line"})
	code, out, _ := run("click", "e1", "--snap")
	if code != 0 || out != "line\nline\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if len(*got) != 2 || (*got)[0].path != "/v1/click" || (*got)[1].path != "/v1/snap" {
		t.Fatalf("requests = %+v", *got)
	}
}

func TestOmitsUnsetOptionalFields(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "x"})
	run("snap")
	b := (*got)[0].body
	if _, ok := b["max"]; ok {
		t.Errorf("max should be omitted when 0: %v", b)
	}
	if _, ok := b["tab"]; ok {
		t.Errorf("tab should be omitted when empty: %v", b)
	}
}

func TestShotWritesPNG(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G'}
	fakeDaemon(t, map[string]any{"ok": true, "text": "ok", "png": base64.StdEncoding.EncodeToString(png)})
	dest := filepath.Join(t.TempDir(), "x.png")
	code, out, errs := run("shot", "-o", dest)
	if code != 0 || strings.TrimSpace(out) != dest {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if b, err := os.ReadFile(dest); err != nil || !bytes.Equal(b, png) {
		t.Fatalf("file = %v, %v", b, err)
	}
}

func TestExitCodes(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	markInstalled(t)
	if code, _, errs := run("click"); code != 2 || !strings.Contains(errs, "usage: greyman click") {
		t.Errorf("missing arg: code=%d err=%q", code, errs)
	}
	if code, _, _ := run("nope"); code != 2 {
		t.Errorf("unknown command: code=%d", code)
	}
	if code, _, errs := run("snap"); code != 1 || !strings.Contains(errs, "greyman up") {
		t.Errorf("not running: code=%d err=%q", code, errs)
	}
	if code, _, _ := run("scroll", "sideways"); code != 2 {
		t.Errorf("bad scroll dir: code=%d", code)
	}
}

func TestDaemonErrorsPrintAndExit1(t *testing.T) {
	fakeDaemon(t, map[string]any{"ok": false, "error": "ref e9 not found or not actionable; run snap"})
	code, _, errs := run("click", "e9")
	if code != 1 || !strings.Contains(errs, "error: ref e9 not found") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestNormalizePlatform(t *testing.T) {
	for in, want := range map[string]string{"": "", "amd64": "linux/amd64", "linux/arm64": "linux/arm64"} {
		if got := normalizePlatform(in); got != want {
			t.Errorf("normalizePlatform(%q) = %q, want %q", in, got, want)
		}
	}
}
