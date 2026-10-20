package docker

import (
	"slices"
	"strings"
	"testing"
)

func TestRunArgs(t *testing.T) {
	o := RunOpts{
		Image: "greyman:latest", Token: "s3cret", Platform: "linux/amd64",
		Env: []string{"GREYMAN_PROXY=http://user:pw@proxy:8080", "GREYMAN_NAV_GAP=1000-2000"},
	}
	args := RunArgs(o)

	for _, a := range args {
		if strings.Contains(a, "s3cret") || strings.Contains(a, "pw@") || strings.Contains(a, "1000-2000") {
			t.Fatalf("value leaked into argv: %q", a)
		}
	}
	for _, want := range []string{"127.0.0.1::7777", "--shm-size=1g", "greyman-profile:/data", "GREYMAN_TOKEN", "GREYMAN_PROXY", "GREYMAN_NAV_GAP"} {
		if !slices.Contains(args, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
	if i := slices.Index(args, "--platform"); i < 0 || args[i+1] != "linux/amd64" {
		t.Errorf("platform not passed: %v", args)
	}
	if args[len(args)-1] != "greyman:latest" {
		t.Errorf("image must be last: %v", args)
	}
}

func TestRunArgsWatchPublishesLoopbackOnly(t *testing.T) {
	if slices.Contains(RunArgs(RunOpts{Image: "x"}), "127.0.0.1::6080") {
		t.Fatal("watch port published without --watch")
	}
	args := RunArgs(RunOpts{Image: "x", Watch: true})
	if !slices.Contains(args, "127.0.0.1::6080") {
		t.Fatalf("watch port not published on loopback: %v", args)
	}
}

func TestRunArgsFixedHostPorts(t *testing.T) {
	args := RunArgs(RunOpts{Image: "x", Watch: true, APIPort: 9000, WatchPort: 9001})
	for _, want := range []string{"127.0.0.1:9000:7777", "127.0.0.1:9001:6080"} {
		if !slices.Contains(args, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
	// A watch port with the live view off publishes nothing.
	for _, a := range RunArgs(RunOpts{Image: "x", WatchPort: 9001}) {
		if strings.Contains(a, "9001") {
			t.Errorf("watch port published without Watch: %q", a)
		}
	}
}

func TestRunArgsNoPlatformByDefault(t *testing.T) {
	if slices.Contains(RunArgs(RunOpts{Image: "x"}), "--platform") {
		t.Fatal("unexpected --platform")
	}
}

func TestRunEnvCarriesSecrets(t *testing.T) {
	env := runEnv(RunOpts{Token: "tok", Env: []string{"GREYMAN_HEADLESS=1", "GREYMAN_PROXY=http://p:1"}})
	for _, want := range []string{"GREYMAN_TOKEN=tok", "GREYMAN_HEADLESS=1", "GREYMAN_PROXY=http://p:1"} {
		if !slices.Contains(env, want) {
			t.Errorf("env missing %q", want)
		}
	}
}

func TestParsePort(t *testing.T) {
	if p, err := parsePort("127.0.0.1:49153\n[::1]:49153"); err != nil || p != 49153 {
		t.Fatalf("parsePort = %d, %v", p, err)
	}
	if _, err := parsePort("garbage"); err == nil {
		t.Fatal("want error")
	}
}
