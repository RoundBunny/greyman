package cli

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"greyman/internal/state"
	"greyman/internal/version"
)

func TestSnapSendsFiltersAndWait(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "x"})
	code, _, errs := run("snap", "--modal", "--grep", "store|zip", "--role", "button,link", "--within", "e7", "--wait", "text=Find", "--wait-timeout", "2000")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	b := (*got)[0].body
	want := map[string]any{"modal": true, "grep": "store|zip", "role": "button,link", "within": "e7", "wait": "text=Find", "waitTimeout": float64(2000)}
	for k, v := range want {
		if b[k] != v {
			t.Errorf("body[%q] = %v, want %v (body %v)", k, b[k], v, b)
		}
	}
}

func TestSnapOmitsUnsetFilters(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "x"})
	run("snap")
	for _, k := range []string{"modal", "grep", "role", "within", "wait", "waitTimeout"} {
		if _, ok := (*got)[0].body[k]; ok {
			t.Errorf("%s should be omitted: %v", k, (*got)[0].body)
		}
	}
}

func TestActionsPassWaitAndSnapKeepsIt(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok"})
	if code, _, errs := run("open", "example.com", "--wait", "idle", "--snap"); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	if b := (*got)[0].body; b["wait"] != "idle" || b["url"] != "example.com" {
		t.Fatalf("open body = %v", b)
	}
	if (*got)[1].path != "/v1/snap" {
		t.Fatalf("requests = %+v", *got)
	}
}

func TestClickByName(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok"})
	if code, _, errs := run("click", "--name", "Use my current location", "--role", "button", "--exact", "--nth", "2"); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	b := (*got)[0].body
	if b["name"] != "Use my current location" || b["role"] != "button" || b["exact"] != true || b["nth"] != float64(2) {
		t.Fatalf("body = %v", b)
	}
	if _, ok := b["ref"]; ok {
		t.Errorf("ref should be absent: %v", b)
	}
}

func TestClickNeedsExactlyOneTarget(t *testing.T) {
	fakeDaemon(t, map[string]any{"ok": true, "text": "ok"})
	for _, args := range [][]string{
		{"click"},
		{"click", "e1", "--name", "x"},
		{"click", "e1", "--role", "button"}, // name options without --name
		{"click", "e1", "e2"},
	} {
		if code, _, errs := run(args...); code != 2 || !strings.Contains(errs, "usage: greyman click") {
			t.Errorf("%v: code=%d err=%q", args, code, errs)
		}
	}
}

func TestReadRefAndModal(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "md"})
	run("read", "e5")
	run("read", "--modal")
	if (*got)[0].body["ref"] != "e5" || (*got)[1].body["modal"] != true {
		t.Fatalf("requests = %+v", *got)
	}
	if code, _, _ := run("read", "e5", "--modal"); code != 2 {
		t.Errorf("ref with --modal: code=%d", code)
	}
}

func TestShotModalAndConflicts(t *testing.T) {
	pngData := tinyPNG(t, 4, 4)
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok", "png": base64.StdEncoding.EncodeToString(pngData)})
	dest := filepath.Join(t.TempDir(), "m.png")
	if code, _, errs := run("shot", "--modal", "-o", dest); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	if (*got)[0].body["modal"] != true {
		t.Fatalf("body = %v", (*got)[0].body)
	}
	for _, args := range [][]string{{"shot", "e1", "--modal"}, {"shot", "--full", "--modal"}, {"shot", "--max-width", "-5"}} {
		if code, _, _ := run(args...); code != 2 {
			t.Errorf("%v: code=%d", args, code)
		}
	}
}

func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 40), uint8(y * 40), 200, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestShotMaxWidthScalesDown(t *testing.T) {
	fakeDaemon(t, map[string]any{"ok": true, "text": "ok", "png": base64.StdEncoding.EncodeToString(tinyPNG(t, 40, 20))})
	dest := filepath.Join(t.TempDir(), "s.png")
	if code, _, errs := run("shot", "--max-width", "10", "-o", dest); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Width != 10 || cfg.Height != 5 {
		t.Fatalf("size = %dx%d, err %v", cfg.Width, cfg.Height, err)
	}
}

func TestDownscaleAveragesAndKeepsNarrowImages(t *testing.T) {
	// 4x2: left half black, right half white -> 2x1 grey-free halves stay black and white.
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			c := color.RGBA{0, 0, 0, 255}
			if x >= 2 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var in bytes.Buffer
	_ = png.Encode(&in, img)
	out, err := downscale(in.Bytes(), 2)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := png.Decode(bytes.NewReader(out))
	if b := got.Bounds(); b.Dx() != 2 || b.Dy() != 1 {
		t.Fatalf("bounds = %v", b)
	}
	if r, _, _, _ := got.At(0, 0).RGBA(); r != 0 {
		t.Errorf("left pixel r = %d, want 0", r)
	}
	if r, _, _, _ := got.At(1, 0).RGBA(); r>>8 != 255 {
		t.Errorf("right pixel r = %d, want 255", r>>8)
	}
	// mixed block averages
	mixed := image.NewRGBA(image.Rect(0, 0, 2, 1))
	mixed.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})
	mixed.SetRGBA(1, 0, color.RGBA{200, 100, 50, 255})
	in.Reset()
	_ = png.Encode(&in, mixed)
	out, _ = downscale(in.Bytes(), 1)
	got, _ = png.Decode(bytes.NewReader(out))
	if r, g, b, _ := got.At(0, 0).RGBA(); r>>8 != 100 || g>>8 != 50 || b>>8 != 25 {
		t.Errorf("average = %d,%d,%d", r>>8, g>>8, b>>8)
	}
	// already narrow enough: returned untouched
	if same, _ := downscale(in.Bytes(), 5); !bytes.Equal(same, in.Bytes()) {
		t.Error("narrow image should be returned as is")
	}
	if _, err := downscale([]byte("not a png"), 1); err == nil {
		t.Error("garbage should fail")
	}
}

func TestSaveWritesFileWithTypeExtension(t *testing.T) {
	img := []byte("\xff\xd8\xffjpegbytes")
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok", "file": base64.StdEncoding.EncodeToString(img), "content_type": "image/jpeg"})
	code, out, errs := run("save", "e12")
	path := strings.TrimSpace(out)
	if code != 0 || !strings.HasSuffix(path, ".jpg") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	defer os.Remove(path)
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, img) {
		t.Fatalf("file = %q, %v", b, err)
	}
	if (*got)[0].path != "/v1/save" || (*got)[0].body["ref"] != "e12" {
		t.Fatalf("request = %+v", (*got)[0])
	}
	dest := filepath.Join(t.TempDir(), "hero.bin")
	if code, out, _ := run("save", "e12", "-o", dest); code != 0 || strings.TrimSpace(out) != dest {
		t.Fatalf("-o: code=%d out=%q", code, out)
	}
}

func TestSaveURLPrintsTheURL(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "https://cdn.test/big.jpg"})
	code, out, _ := run("save", "e12", "--url")
	if code != 0 || strings.TrimSpace(out) != "https://cdn.test/big.jpg" || (*got)[0].body["url"] != true {
		t.Fatalf("code=%d out=%q body=%v", code, out, (*got)[0].body)
	}
	for _, args := range [][]string{{"save"}, {"save", "e1", "e2"}, {"save", "e1", "--url", "-o", "x"}} {
		if code, _, _ := run(args...); code != 2 {
			t.Errorf("%v: code=%d", args, code)
		}
	}
}

func TestLogFlags(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "(no log entries)"})
	code, out, _ := run("log", "--errors", "--max", "5", "--clear", "--tab", "t2")
	b := (*got)[0].body
	if code != 0 || !strings.Contains(out, "no log entries") || (*got)[0].path != "/v1/log" ||
		b["errors"] != true || b["max"] != float64(5) || b["clear"] != true || b["tab"] != "t2" {
		t.Fatalf("code=%d out=%q body=%v", code, out, b)
	}
	if code, _, _ := run("log", "extra"); code != 2 {
		t.Errorf("positional: code=%d", code)
	}
}

// daemonAt starts a fake daemon that answers with the given X-Greyman-Version.
func daemonAt(t *testing.T, header *string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header != nil {
			w.Header().Set("X-Greyman-Version", *header)
		}
		_, _ = w.Write([]byte(`{"ok":true,"text":"x"}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	markInstalled(t)
	if err := state.Save(&state.State{Container: "greyman", Port: port, Token: "t"}); err != nil {
		t.Fatal(err)
	}
}

func captureVersionWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := versionWarn
	versionWarn, versionOnce = &buf, sync.Once{}
	t.Cleanup(func() { versionWarn, versionOnce = old, sync.Once{} })
	return &buf
}

func TestVersionMismatchWarnsOnceAndStillRuns(t *testing.T) {
	buf := captureVersionWarnings(t)
	other := version.Version + "-other"
	daemonAt(t, &other)
	if code, out, _ := run("snap"); code != 0 || strings.TrimSpace(out) != "x" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	run("snap")
	w := buf.String()
	if strings.Count(w, "warning:") != 1 || !strings.Contains(w, version.Version) || !strings.Contains(w, other) || !strings.Contains(w, "greyman down") {
		t.Fatalf("warning = %q", w)
	}
}

func TestMatchingVersionIsSilent(t *testing.T) {
	buf := captureVersionWarnings(t)
	same := version.Version
	daemonAt(t, &same)
	run("snap")
	if buf.Len() != 0 {
		t.Fatalf("warning = %q", buf.String())
	}
}

func TestDaemonWithoutVersionHeaderIsStale(t *testing.T) {
	buf := captureVersionWarnings(t)
	daemonAt(t, nil)
	run("snap")
	if !strings.Contains(buf.String(), "predates version reporting") {
		t.Fatalf("warning = %q", buf.String())
	}
}
