package cli

import (
	"strings"
	"testing"
)

func TestSetGeolocationIsLiveOnly(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok geolocation=40.7,-74 accuracy=10"})
	before := cfgFile(t)
	// A negative longitude must not be read as a flag.
	code, out, errs := run("set", "geolocation", "40.7", "-74")
	if code != 0 || out != "ok geolocation=40.7,-74 accuracy=10\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	r := (*got)[0]
	if r.path != "/v1/set" || r.body["option"] != "geolocation" || r.body["latitude"] != 40.7 || r.body["longitude"] != -74.0 {
		t.Fatalf("request = %+v", r)
	}
	if _, ok := r.body["accuracy"]; ok {
		t.Errorf("accuracy should be left to the daemon when not given: %v", r.body)
	}
	if cfgFile(t) != before {
		t.Errorf("set without --setconfig changed options.cfg:\n%s", cfgFile(t))
	}
}

func TestSetGeolocationSetconfigSavesWhatItApplied(t *testing.T) {
	// The flag may come first, last, or between the values. Each case gets a fresh
	// config, so a case cannot pass on what an earlier one wrote.
	for name, args := range map[string][]string{
		"flag last":  {"set", "geolocation", "-33.8688", "151.2093", "25", "--setconfig"},
		"flag first": {"set", "--setconfig", "geolocation", "-33.8688", "151.2093", "25"},
		"flag among": {"set", "geolocation", "-33.8688", "--setconfig", "151.2093", "25"},
	} {
		t.Run(name, func(t *testing.T) {
			got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok geolocation=-33.8688,151.2093 accuracy=25"})
			code, out, errs := run(args...)
			if code != 0 || out != "ok geolocation=-33.8688,151.2093 accuracy=25 saved=options.cfg\n" {
				t.Fatalf("code=%d out=%q err=%q", code, out, errs)
			}
			for _, want := range []string{
				"\nUSE_GEOLOCATION=1\n", "\nGEOLOCATION_COORD_LAT=-33.8688\n", "\nGEOLOCATION_COORD_LON=151.2093\n", "\nGEOLOCATION_ACCURACY=25\n",
			} {
				if !strings.Contains(cfgFile(t), want) {
					t.Errorf("options.cfg missing %q:\n%s", want, cfgFile(t))
				}
			}
			if r := (*got)[0]; r.body["accuracy"] != 25.0 {
				t.Errorf("request = %+v", r)
			}
		})
	}
}

func TestSetSetconfigWarnsWhenTheEnvironmentOverridesAKey(t *testing.T) {
	fakeDaemon(t, map[string]any{"ok": true, "text": "ok"})
	t.Setenv("GREYMAN_GEOLOCATION_COORD_LAT", "5")
	t.Setenv("GREYMAN_USE_GEOLOCATION", "")
	code, out, errs := run("set", "geolocation", "1", "2", "--setconfig")
	if code != 0 || out != "ok saved=options.cfg\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if !strings.Contains(errs, "warning: GREYMAN_GEOLOCATION_COORD_LAT is set in the environment") {
		t.Errorf("no warning for the overridden key: %q", errs)
	}
	if strings.Contains(errs, "GREYMAN_USE_GEOLOCATION") || strings.Contains(errs, "GREYMAN_GEOLOCATION_COORD_LON") {
		t.Errorf("warned about a key the environment does not set: %q", errs)
	}
	// Without --setconfig nothing is saved, so there is nothing to warn about.
	if _, _, errs := run("set", "geolocation", "1", "2"); errs != "" {
		t.Errorf("warning without --setconfig: %q", errs)
	}
}

func TestSetGeolocationSetconfigLeavesAccuracyAloneWhenOmitted(t *testing.T) {
	fakeDaemon(t, map[string]any{"ok": true, "text": "ok"})
	if code, _, errs := run("set", "geolocation", "1", "2", "--setconfig"); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	if strings.Contains(cfgFile(t), "GEOLOCATION_ACCURACY") {
		t.Errorf("accuracy was written though none was given:\n%s", cfgFile(t))
	}
}

func TestSetGeolocationOff(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok geolocation=off"})
	if code, out, errs := run("set", "geolocation", "off"); code != 0 || out != "ok geolocation=off\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if r := (*got)[0]; r.body["off"] != true || r.body["option"] != "geolocation" {
		t.Fatalf("request = %+v", r)
	}
	if strings.Contains(cfgFile(t), "GEOLOCATION") {
		t.Errorf("off without --setconfig changed options.cfg:\n%s", cfgFile(t))
	}
	if code, _, errs := run("set", "geolocation", "off", "--setconfig"); code != 0 {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	f := cfgFile(t)
	if !strings.Contains(f, "\nUSE_GEOLOCATION=0\n") || strings.Contains(f, "COORD") {
		t.Errorf("off --setconfig should only turn USE_GEOLOCATION off:\n%s", f)
	}
}

func TestSetRejectsBadInputBeforeAnyRequest(t *testing.T) {
	got := fakeDaemon(t, map[string]any{"ok": true, "text": "ok"})
	before := cfgFile(t)
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"set"}, 2, "usage: greyman set"},
		{[]string{"set", "geolocation"}, 2, "usage: greyman set geolocation"},
		{[]string{"set", "geolocation", "10"}, 2, "usage: greyman set geolocation"},
		{[]string{"set", "geolocation", "1", "2", "3", "4"}, 2, "usage: greyman set geolocation"},
		{[]string{"set", "geolocation", "91", "0"}, 1, "invalid latitude"},
		{[]string{"set", "geolocation", "0", "-181"}, 1, "invalid longitude"},
		{[]string{"set", "geolocation", "0", "0", "-1"}, 1, "invalid accuracy"},
		// The error names the argument that is wrong, wherever the numbers are.
		{[]string{"set", "geolocation", "10", "abc"}, 1, "invalid longitude"},
		{[]string{"set", "geolocation", "abc", "10"}, 1, "invalid latitude"},
		{[]string{"set", "geolocation", "1e1", "0"}, 1, "invalid latitude"},
		{[]string{"set", "nope", "1", "2"}, 1, `unknown option "nope"`},
		{[]string{"set", "geolocation", "1", "2", "--nope"}, 2, "not defined"},
	}
	for _, c := range cases {
		args := append(c.args, "--setconfig")
		code, _, errs := run(args...)
		if code != c.code || !strings.Contains(errs, c.want) {
			t.Errorf("%v: code=%d err=%q, want code %d with %q", args, code, errs, c.code, c.want)
		}
	}
	if len(*got) != 0 {
		t.Errorf("daemon was called: %+v", *got)
	}
	if cfgFile(t) != before {
		t.Errorf("options.cfg changed:\n%s", cfgFile(t))
	}
}

func TestSetDoesNotSaveWhenTheDaemonRefuses(t *testing.T) {
	fakeDaemon(t, map[string]any{"ok": false, "error": "latitude must be a number from -90 to 90"})
	before := cfgFile(t)
	code, out, errs := run("set", "geolocation", "10", "20", "--setconfig")
	if code != 1 || out != "" || !strings.Contains(errs, "error: latitude must be") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if cfgFile(t) != before {
		t.Errorf("options.cfg changed though the daemon refused:\n%s", cfgFile(t))
	}
}

func TestSetNeedsARunningBrowser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	markInstalled(t)
	before := cfgFile(t)
	code, _, errs := run("set", "geolocation", "10", "20", "--setconfig")
	if code != 1 || !strings.Contains(errs, "greyman up") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	if cfgFile(t) != before {
		t.Errorf("options.cfg changed with nothing running:\n%s", cfgFile(t))
	}
}

func TestConfigAcceptsNegativeValues(t *testing.T) {
	setup(t)
	if code, out, errs := run("config", "set", "GEOLOCATION_COORD_LON", "-122.4194"); code != 0 || out != "ok GEOLOCATION_COORD_LON=-122.4194\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if code, out, _ := run("config", "get", "GEOLOCATION_COORD_LON"); code != 0 || out != "-122.4194\n" {
		t.Fatalf("get: code=%d out=%q", code, out)
	}
	if code, _, errs := run("config", "set", "GEOLOCATION_COORD_LON", "-181"); code != 1 || !strings.Contains(errs, "invalid GEOLOCATION_COORD_LON") {
		t.Fatalf("out of range: code=%d err=%q", code, errs)
	}
}
