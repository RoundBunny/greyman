package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"greyman/internal/client"
	"greyman/internal/docker"
	"greyman/internal/settings"
	"greyman/internal/state"
	"greyman/internal/version"
)

// confirm asks a y/N question on the terminal. With no terminal on stdin (an agent, a
// pipe, /dev/null) it answers no without asking, so nothing waits on input that never comes. Tests
// swap it.
var confirm = func(e *Env, question string) bool {
	if !isTerminal(os.Stdin) {
		return false
	}
	fmt.Fprintf(e.Err, "%s [y/N] ", question)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// buildMissing offers to build a release's own image, which the install script normally
// builds. Declined or with no terminal, it prints the docker command instead. Any other
// missing image is the user's to provide.
func buildMissing(ctx context.Context, e *Env, image string) error {
	v := version.Version
	switch {
	case image != docker.ImageFor(v):
		return fmt.Errorf("image %s not found", image)
	case !version.IsRelease(v):
		return fmt.Errorf("image %s not found; build it from the repo root with:\n  docker build -f docker/Dockerfile --build-arg VERSION=%s -t %s .", image, v, image)
	case !confirm(e, fmt.Sprintf("image %s not found. Build it now (~500 MB download, several minutes)?", image)):
		return fmt.Errorf("image %s not found; build it (~500 MB download, several minutes) with:\n  docker %s", image, strings.Join(docker.BuildArgs(v, image), " "))
	}
	return docker.Build(ctx, v, image, e.Err)
}

// upFlagKeys maps the `up` flags to the settings they override.
var upFlagKeys = map[string]string{
	"headless": "HEADLESS", "watch": "WATCH", "proxy": "PROXY", "platform": "PLATFORM", "image": "IMAGE",
}

// givenFlags returns the settings whose flags were passed, by key. A flag left at its
// default is absent, so it does not override options.cfg.
func givenFlags(fs *flag.FlagSet) map[string]string {
	given := map[string]string{}
	fs.Visit(func(f *flag.Flag) {
		v := f.Value.String()
		if on, ok := f.Value.(flag.Getter).Get().(bool); ok {
			v = "0"
			if on {
				v = "1"
			}
		}
		given[upFlagKeys[f.Name]] = v
	})
	return given
}

func cmdUp(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman up [--headless | --watch] [--proxy URL] [--platform amd64] [--image NAME]"
	fs := newFlags("up", e, usage)
	fs.Bool("headless", false, "run Chromium headless instead of headed under Xvfb")
	fs.Bool("watch", false, "serve a view-only live view of the browser (noVNC) on loopback; headed only")
	fs.String("proxy", "", "upstream proxy URL for browser traffic")
	fs.String("platform", "", "container platform, e.g. amd64 (needs a matching image)")
	fs.String("image", docker.DefaultImage, "image to run")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageError{usage}
	}

	given := givenFlags(fs)
	if strings.Contains(given["PROXY"], "@") {
		return errors.New("proxy credentials would show up in `ps`; set GREYMAN_PROXY in the environment or PROXY in options.cfg instead")
	}
	cfg, err := settings.Resolve(given)
	if err != nil {
		return err
	}
	if w, h := cfg["WATCH"], cfg["HEADLESS"]; w.Bool() && h.Bool() {
		return fmt.Errorf("watch needs headed mode, but headless is set (WATCH from %s, HEADLESS from %s)", w.Source, h.Source)
	}
	if cfg["WATCH"].Bool() && cfg["API_PORT"].Int() != 0 && cfg["API_PORT"].Val == cfg["WATCH_PORT"].Val {
		return errors.New("API_PORT and WATCH_PORT must differ")
	}

	if st, err := state.Load(); err == nil && docker.Running(ctx, st.Container) {
		return printStatus(ctx, e, st)
	}
	for _, w := range settings.Warnings(cfg) {
		fmt.Fprintln(e.Err, "warning:", w)
	}
	image := cfg["IMAGE"].Val
	if !docker.ImageExists(ctx, image) {
		if err := buildMissing(ctx, e, image); err != nil {
			return err
		}
	}
	// Without state the token is lost, so this container is unusable, but it may be a
	// live session (another HOME, a deleted state file). Let the user decide.
	if docker.Running(ctx, docker.Name) {
		return errors.New("a greyman container is running but its state file is missing; run `greyman down` first")
	}
	// Clear any exited container from a crashed run; it holds the name.
	if err := docker.Remove(ctx, docker.Name, false); err != nil {
		return err
	}

	token, err := newToken()
	if err != nil {
		return err
	}
	opts := docker.RunOpts{
		Image:     image,
		Platform:  normalizePlatform(cfg["PLATFORM"].Val),
		Token:     token,
		Env:       settings.ContainerEnv(cfg),
		Watch:     cfg["WATCH"].Bool(),
		APIPort:   cfg["API_PORT"].Int(),
		WatchPort: cfg["WATCH_PORT"].Int(),
	}
	ports, err := docker.Run(ctx, opts)
	if err != nil {
		abandon(ctx)
		return err
	}
	st := &state.State{Container: docker.Name, Port: ports.API, WatchPort: ports.Watch, Token: token}
	if err := state.Save(st); err != nil {
		abandon(ctx)
		return err
	}

	text, err := waitReady(ctx, client.New(st.Port, token), 90*time.Second)
	if err != nil {
		abandon(ctx)
		return err
	}
	fmt.Fprintln(e.Out, text+watchSuffix(st))
	return nil
}

// abandon tears down a container that failed to start, so the next `up` starts clean
// instead of finding a running container it cannot talk to. It runs even if ctx was
// cancelled (Ctrl-C) and ignores its own errors: the caller is already returning one.
func abandon(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	_ = docker.Remove(ctx, docker.Name, false)
	_ = state.Remove()
}

// watchSuffix is " watch=<url>" when the live view is enabled, else "".
func watchSuffix(st *state.State) string {
	if st.WatchPort == 0 {
		return ""
	}
	return fmt.Sprintf(" watch=http://127.0.0.1:%d/vnc.html?autoconnect=true&view_only=true&resize=scale", st.WatchPort)
}

func cmdDown(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman down [--purge]"
	fs := newFlags("down", e, usage)
	purge := fs.Bool("purge", false, "also delete the persistent profile (cookies, storage)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageError{usage}
	}
	if err := docker.Remove(ctx, docker.Name, *purge); err != nil {
		return err
	}
	if err := state.Remove(); err != nil {
		return err
	}
	fmt.Fprintln(e.Out, "ok down")
	return nil
}

func cmdStatus(ctx context.Context, e *Env, args []string) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	return printStatus(ctx, e, st)
}

func printStatus(ctx context.Context, e *Env, st *state.State) error {
	resp, err := client.New(st.Port, st.Token).Do(ctx, "status", nil)
	checkReply(resp, err)
	if err != nil {
		return explain(ctx, err)
	}
	fmt.Fprintln(e.Out, resp.Text+watchSuffix(st))
	return nil
}

// waitReady polls the daemon until it answers, failing fast if the container dies.
func waitReady(ctx context.Context, c *client.Client, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := c.Do(ctx, "status", nil)
		if err == nil {
			return resp.Text, nil
		}
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			return "", err // daemon answered with an error (e.g. 401): retrying won't help
		}
		if !docker.Running(ctx, docker.Name) {
			return "", fmt.Errorf("container exited during startup:\n%s", docker.Logs(ctx, docker.Name, 20))
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for the daemon:\n%s", docker.Logs(ctx, docker.Name, 20))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// "amd64" -> "linux/amd64"
func normalizePlatform(p string) string {
	if p == "" || strings.Contains(p, "/") {
		return p
	}
	return "linux/" + p
}
