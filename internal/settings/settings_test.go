package settings

import (
	"strings"
	"testing"

	"greyman/internal/config"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	// Detection reads these; pin them so results do not depend on the host.
	t.Setenv("TZ", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "C")
	for _, s := range All {
		t.Setenv("GREYMAN_"+s.Key, "")
	}
}

func TestDefaults(t *testing.T) {
	isolate(t)
	got, err := Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range All {
		if got[s.Key].Source != Default {
			t.Errorf("%s source = %s, want default", s.Key, got[s.Key].Source)
		}
	}
	if got["NAV_GAP"].Val != "3000-6000" || got["API_PORT"].Int() != 0 || got["HEADLESS"].Bool() {
		t.Errorf("unexpected defaults: %v", got)
	}
}

func TestPrecedenceFlagThenEnvThenFileThenDefault(t *testing.T) {
	isolate(t)
	if err := config.Set("MAX_CHARS", "100"); err != nil {
		t.Fatal(err)
	}
	check := func(flags map[string]string, wantVal string, wantSrc Source) {
		t.Helper()
		got, err := Resolve(flags)
		if err != nil {
			t.Fatal(err)
		}
		if v := got["MAX_CHARS"]; v.Val != wantVal || v.Source != wantSrc {
			t.Errorf("MAX_CHARS = %+v, want %s from %s", v, wantVal, wantSrc)
		}
	}
	check(nil, "100", File)
	t.Setenv("GREYMAN_MAX_CHARS", "200")
	check(nil, "200", Env)
	check(map[string]string{"MAX_CHARS": "300"}, "300", Flag)
}

func TestEmptyValuesCountAsUnset(t *testing.T) {
	isolate(t)
	if err := config.Set("MAX_CHARS", "100"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GREYMAN_MAX_CHARS", "")
	got, err := Resolve(map[string]string{"MAX_CHARS": ""})
	if err != nil || got["MAX_CHARS"].Val != "100" {
		t.Fatalf("MAX_CHARS = %+v, %v", got["MAX_CHARS"], err)
	}
}

func TestOnlyTheWinningValueIsValidated(t *testing.T) {
	isolate(t)
	if err := config.Set("NAV_GAP", "oops"); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(nil); err == nil || !strings.Contains(err.Error(), "NAV_GAP") || !strings.Contains(err.Error(), "options.cfg") {
		t.Fatalf("err = %v, want one naming NAV_GAP and options.cfg", err)
	}
	t.Setenv("GREYMAN_NAV_GAP", "1000-2000")
	if got, err := Resolve(nil); err != nil || got["NAV_GAP"].Val != "1000-2000" {
		t.Fatalf("env should shadow the bad file line: %+v, %v", got["NAV_GAP"], err)
	}
	t.Setenv("GREYMAN_NAV_GAP", "9-1")
	if _, err := Resolve(nil); err == nil || !strings.Contains(err.Error(), "$GREYMAN_NAV_GAP") {
		t.Fatalf("err = %v, want one naming $GREYMAN_NAV_GAP", err)
	}
}

func TestInvalidProxyErrorDoesNotEchoTheValue(t *testing.T) {
	isolate(t)
	if err := config.Set("PROXY", "http://user:hunter2@"); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(nil)
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidators(t *testing.T) {
	cases := []struct {
		key  string
		good []string
		bad  []string
	}{
		{"NAV_GAP", []string{"0-0", "3000-6000"}, []string{"6000-3000", "3000", "a-b", "-1-5", "1-2-3", " 1-2"}},
		{"MAX_CHARS", []string{"1", "24000"}, []string{"0", "-5", "1.5", "abc"}},
		{"SNAP_MAX_TEXT", []string{"1", "200"}, []string{"0", "abc"}},
		{"ACTION_TIMEOUT_MS", []string{"5000"}, []string{"0", "-1"}},
		{"NAV_TIMEOUT_MS", []string{"30000"}, []string{"0", "5s"}},
		{"DEFAULT_WAIT", []string{"1", "3000"}, []string{"0", "-1", "3s", "1.5"}},
		{"PROXY", []string{"proxy:3128", "http://u:p@proxy:3128", "socks5://127.0.0.1:1080"}, []string{"http://", "://x", "http://[::1"}},
		{"LOCALE", []string{"en-US", "fr", "zh-Hant-TW"}, []string{"en_US", "e", "en-"}},
		{"TZ", []string{"America/Chicago", "UTC"}, []string{"Mars/Base", "Local", "CST"}},
		{"HEADLESS", []string{"0", "1"}, []string{"true", "2", "yes"}},
		{"USE_GEOLOCATION", []string{"0", "1"}, []string{"true", "2"}},
		{"GEOLOCATION_COORD_LAT", []string{"0", "37.7749", "-90", "90", "-0.5"}, []string{"90.1", "-91", "abc", "1e1", "0x10", "NaN", "Inf", "+5", ".5", "5.", ""}},
		{"GEOLOCATION_COORD_LON", []string{"0", "-122.4194", "-180", "180"}, []string{"180.1", "-181", "east", "1e2", "-"}},
		{"GEOLOCATION_ACCURACY", []string{"0", "10", "2.5", "5000"}, []string{"-1", "abc", "1e3", "Inf"}},
		{"API_PORT", []string{"0", "1", "65535"}, []string{"-1", "65536", "http"}},
		{"IMAGE", []string{"greyman:latest", "ghcr.io/x/y:1"}, []string{"a b"}},
		{"PLATFORM", []string{"amd64", "linux/arm64", "linux/arm/v7"}, []string{"Linux/AMD64", "linux/", "a b"}},
	}
	byKey := map[string]Setting{}
	for _, s := range All {
		byKey[s.Key] = s
	}
	for _, c := range cases {
		s, ok := byKey[c.key]
		if !ok {
			t.Fatalf("no setting %s", c.key)
		}
		for _, v := range c.good {
			if err := s.Validate(v); err != nil {
				t.Errorf("%s accepts %q: got %v", c.key, v, err)
			}
		}
		for _, v := range c.bad {
			if err := s.Validate(v); err == nil {
				t.Errorf("%s rejected %q: got nil", c.key, v)
			}
		}
	}
}

func TestEveryDefaultPassesItsOwnValidator(t *testing.T) {
	for _, s := range All {
		if s.Default == "" {
			continue
		}
		if err := s.Validate(s.Default); err != nil {
			t.Errorf("%s default %q: %v", s.Key, s.Default, err)
		}
	}
}

func TestContainerEnv(t *testing.T) {
	isolate(t)
	t.Setenv("TZ", "Europe/Paris")
	t.Setenv("LANG", "en_GB.UTF-8")
	if err := config.Set("PROXY", "http://u:p@proxy:3128"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set("API_PORT", "9000"); err != nil {
		t.Fatal(err)
	}
	vals, err := Resolve(map[string]string{"HEADLESS": "1"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ContainerEnv(vals), "\n")
	for _, want := range []string{
		"GREYMAN_NAV_GAP=3000-6000", "GREYMAN_ACTION_TIMEOUT_MS=5000", "GREYMAN_NAV_TIMEOUT_MS=30000", "GREYMAN_DEFAULT_WAIT=3000",
		"GREYMAN_SNAP_MAX_TEXT=200", "GREYMAN_PROXY=http://u:p@proxy:3128", "GREYMAN_HEADLESS=1",
		"GREYMAN_WATCH=0", "GREYMAN_SCREEN_W=1920", "TZ=Europe/Paris", "GREYMAN_LOCALE=en-GB",
		"GREYMAN_USE_GEOLOCATION=0", "GREYMAN_GEOLOCATION_COORD_LAT=0", "GREYMAN_GEOLOCATION_COORD_LON=0",
		"GREYMAN_GEOLOCATION_ACCURACY=10",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("env missing %q:\n%s", want, got)
		}
	}
	// Host-only settings stay on the host; unset ones are not passed as empty strings.
	for _, bad := range []string{"API_PORT", "IMAGE", "PLATFORM", "GREYMAN_PLATFORM="} {
		if strings.Contains(got, bad) {
			t.Errorf("env contains %q:\n%s", bad, got)
		}
	}
}

func TestHostLocale(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := hostLocale(); got != "en-US" {
		t.Fatalf("hostLocale = %q", got)
	}
	t.Setenv("LANG", "C")
	if got := hostLocale(); got != "" {
		t.Fatalf("hostLocale(C) = %q", got)
	}
}

func TestHostTZPrefersTheEnvironment(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	if got := hostTZ(); got != "Asia/Tokyo" {
		t.Fatalf("hostTZ = %q", got)
	}
}

func TestDisplayMasksProxyPasswords(t *testing.T) {
	for in, want := range map[string]string{
		"http://u:hunter2@proxy:3128": "http://u:***@proxy:3128",
		"u:hunter2@proxy:3128":        "u:***@proxy:3128",
		"socks5://:pw@h:1":            "socks5://:***@h:1",
		"http://proxy:3128":           "http://proxy:3128",
		"proxy:3128":                  "proxy:3128",
		"http://u@proxy:3128":         "http://u@proxy:3128",
		"http://u:p@ss@h:1/x":         "http://u:***@h:1/x",
		"http://user:p/ss@host:80":    "http://user:***@host:80",
		"user:p/ss@host:80":           "user:***@host:80",
	} {
		if got := Display("PROXY", in); got != want {
			t.Errorf("Display(PROXY, %q) = %q, want %q", in, got, want)
		}
	}
	if got := Display("NAV_GAP", "a:b@c"); got != "a:b@c" {
		t.Errorf("other keys must pass through, got %q", got)
	}
}

func TestWarningsFlagDoubtfulDetectedDefaults(t *testing.T) {
	has := func(ws []string, sub string) bool {
		for _, w := range ws {
			if strings.Contains(w, sub) {
				return true
			}
		}
		return false
	}
	if ws := Warnings(map[string]Value{"TZ": {"", Default}}); !has(ws, "could not detect the host time zone") {
		t.Errorf("undetected TZ: %q", ws)
	}
	if ws := Warnings(map[string]Value{"TZ": {":America/Chicago", Default}}); !has(ws, `detected host TZ ":America/Chicago"`) {
		t.Errorf("invalid detected TZ: %q", ws)
	}
	on := map[string]Value{"USE_GEOLOCATION": {"1", File}, "GEOLOCATION_COORD_LAT": {"0", Default}, "GEOLOCATION_COORD_LON": {"0", Default}}
	if ws := Warnings(on); !has(ws, "USE_GEOLOCATION is on but no coordinates are set") {
		t.Errorf("geolocation on with default coordinates: %q", ws)
	}
	for name, vals := range map[string]map[string]Value{
		"geolocation off":    {"USE_GEOLOCATION": {"0", Default}},
		"geolocation lat":    {"USE_GEOLOCATION": {"1", File}, "GEOLOCATION_COORD_LAT": {"40", File}},
		"geolocation coords": {"USE_GEOLOCATION": {"1", File}, "GEOLOCATION_COORD_LAT": {"40", File}, "GEOLOCATION_COORD_LON": {"0", File}},
		"valid":              {"TZ": {"America/Chicago", Default}, "LOCALE": {"en-US", Default}},
		"unset locale":       {"LOCALE": {"", Default}},
		"explicit value":     {"TZ": {"nonsense", File}},
	} {
		if ws := Warnings(vals); len(ws) != 0 {
			t.Errorf("%s: unexpected warnings %q", name, ws)
		}
	}
}

func TestInspectReportsInvalidValuesInsteadOfFailing(t *testing.T) {
	isolate(t)
	if err := config.Set("NAV_GAP", "oops"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set("MAX_CHARS", "5"); err != nil {
		t.Fatal(err)
	}
	entries, err := Inspect()
	if err != nil || len(entries) != len(All) {
		t.Fatalf("Inspect = %d entries, %v", len(entries), err)
	}
	for _, e := range entries {
		switch e.Setting.Key {
		case "NAV_GAP":
			if e.Err == nil || e.Value.Val != "oops" || e.Value.Source != File {
				t.Errorf("NAV_GAP = %+v, err %v", e.Value, e.Err)
			}
		case "MAX_CHARS":
			if e.Err != nil || e.Value.Val != "5" {
				t.Errorf("MAX_CHARS = %+v, err %v", e.Value, e.Err)
			}
		}
	}
}
