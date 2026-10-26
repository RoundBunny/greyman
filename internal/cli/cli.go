// Package cli implements the greyman subcommands.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"greyman/internal/client"
	"greyman/internal/docker"
	"greyman/internal/state"
	"greyman/internal/version"
)

type Env struct {
	Out, Err io.Writer
}

type command struct {
	name, usage, summary string
	run                  func(ctx context.Context, e *Env, args []string) error
}

// usageError makes Run exit 2 and print the command's usage line.
type usageError struct{ usage string }

func (u usageError) Error() string { return "usage: " + u.usage }

// flagError is a bad flag. The flag package has already printed the problem and the
// usage, so Run only needs to exit 2.
type flagError struct{ error }

// ungated commands run before any agent is installed. Every other command is gated
// by default, so a new command cannot skip the check by accident.
var ungated = map[string]bool{"install": true, "down": true, "config": true, "version": true}

func commands() []command {
	return []command{
		{"up", "greyman up [--headless | --watch] [--proxy URL] [--platform amd64] [--image NAME]", "start the browser container (--watch: live view URL)", cmdUp},
		{"down", "greyman down [--purge]", "stop the container (--purge also wipes cookies/profile)", cmdDown},
		{"status", "greyman status", "show container state, open tabs and version", cmdStatus},
		{"open", "greyman open <url> [--tab tN] [--snap] " + waitUsage, "navigate a tab to a URL (https:// is added if missing)", cmdOpen},
		{"snap", "greyman snap [--modal] [--grep RE] [--role R] [--within REF|RE] [--max N] " + waitUsage + " [--tab tN]", "compact accessibility tree with refs (primary view)", cmdSnap},
		{"click", "greyman click <ref> | --name TEXT [--role R] [--exact] [--nth N] [--tab tN] [--snap] " + waitUsage, "click an element by ref or by name", cmdClick},
		{"type", "greyman type <ref> <text> [--submit] [--tab tN] [--snap] " + waitUsage, "type into an element (replaces content; --submit presses Enter)", cmdType},
		{"scroll", "greyman scroll <up|down> [n] [--tab tN] [--snap] " + waitUsage, "scroll n screens (default 1, at most 20)", cmdScroll},
		{"back", "greyman back [--tab tN] [--snap] " + waitUsage, "go back in history", cmdBack},
		{"read", "greyman read [ref] [--modal] [--tab tN] [--max N]", "page, dialog or element content as markdown (when snap is not enough)", cmdRead},
		{"shot", "greyman shot [ref] [--modal] [--full] [--max-width N] [-o path] [--tab tN]", "screenshot to a PNG file; prints the path", cmdShot},
		{"save", "greyman save <ref> [-o path] [--url] [--tab tN]", "download the image behind a ref at full size; prints the path", cmdSave},
		{"log", "greyman log [--errors] [--max N] [--clear] [--tab tN]", "console errors, exceptions, failed requests and navigations of a tab", cmdLog},
		{"close", "greyman close [--tab tN]", "close a tab (closing the last one stops the browser)", cmdClose},
		{"set", "greyman set <option> <args> [--setconfig]", "change a setting on the running browser, e.g. geolocation (--setconfig also saves it)", cmdSet},
		{"config", "greyman config [list | get KEY | set KEY VALUE | unset KEY]", "show or change settings (applied on the next `up`)", cmdConfig},
		{"install", "greyman install [claude|codex|cursor|copilot|all|other [dir]] [--force]", "install the greyman skill for a coding agent (no name: detect)", cmdInstall},
		{"version", "greyman version", "print the CLI version", cmdVersion},
	}
}

func cmdVersion(_ context.Context, e *Env, args []string) error {
	pos, err := parse(newFlags("version", e, "greyman version"), args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageError{"greyman version"}
	}
	fmt.Fprintln(e.Out, version.Version)
	return nil
}

// Run executes one greyman invocation and returns the process exit code.
func Run(ctx context.Context, args []string, out, errw io.Writer) int {
	e := &Env{Out: out, Err: errw}
	if len(args) == 0 {
		e.help()
		return 2
	}
	if name := args[0]; name == "help" || name == "-h" || name == "--help" {
		e.help()
		return 0
	}
	for _, c := range commands() {
		if c.name != args[0] {
			continue
		}
		var err error
		if !ungated[c.name] {
			err = requireSetup()
		}
		if err == nil {
			err = c.run(ctx, e, args[1:])
		}
		var u usageError
		var fe flagError
		switch {
		case err == nil, errors.Is(err, flag.ErrHelp):
			return 0
		case errors.As(err, &fe):
			return 2
		case errors.Is(err, context.Canceled):
			fmt.Fprintln(errw, "interrupted")
			return 130 // 128 + SIGINT, as shells report it
		case errors.As(err, &u):
			fmt.Fprintln(errw, u.Error())
			return 2
		default:
			fmt.Fprintf(errw, "error: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(errw, "unknown command %q\n", args[0])
	e.help()
	return 2
}

// helpNotes explains what the usage lines share.
const helpNotes = `
W is idle (the default), text=TEXT, css=SELECTOR or none; --wait-timeout caps it
(default: the DEFAULT_WAIT setting). A timeout is reported, not an error.
Refs like e12 come from snap; after the page changes, run snap again.
--tab tN picks a tab (default: the active one). Flags may come before or after
arguments; use -- before text that starts with -.
`

func (e *Env) help() {
	var b strings.Builder
	b.WriteString("greyman: a browser for coding agents\n\n")
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %-58s %s\n", c.usage, c.summary)
	}
	b.WriteString(helpNotes)
	fmt.Fprint(e.Err, b.String())
}

// newFlags returns a FlagSet that prints problems and the usage; parse turns them into
// exit code 2.
func newFlags(name string, e *Env, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.Err)
	fs.Usage = func() {
		fmt.Fprintf(e.Err, "usage: %s\n", usage)
		fs.PrintDefaults()
	}
	return fs
}

// parse allows flags before or after positional args (`type e3 hello --submit`).
// Everything after a literal "--" is positional, so text may start with "-".
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var tail []string
	for i, a := range args {
		if a == "--" {
			args, tail = args[:i], args[i+1:]
			break
		}
	}
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, flagError{err}
		}
		if fs.NArg() == 0 {
			return append(pos, tail...), nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// parseNumbers is parse for commands whose flags take no value and whose positionals
// can be negative numbers (a longitude): a number is positional even with a leading
// "-". Positionals keep their order.
func parseNumbers(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, pos []string
	for _, a := range args {
		if _, err := strconv.ParseFloat(a, 64); err == nil || !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
		} else {
			flags = append(flags, a)
		}
	}
	rest, err := parse(fs, flags)
	return append(pos, rest...), err
}

// connect builds a client from the saved state.
func connect() (*client.Client, error) {
	st, err := state.Load()
	if err != nil {
		return nil, err
	}
	return client.New(st.Port, st.Token), nil
}

// call sends one command and returns the response.
func call(ctx context.Context, name string, args map[string]any) (*client.Response, error) {
	c, err := connect()
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, name, args)
	checkReply(resp, err)
	return resp, explain(ctx, err)
}

var (
	// versionWarn receives the version mismatch warning; tests swap it.
	versionWarn io.Writer = os.Stderr
	versionOnce sync.Once
)

// checkVersion warns, once per process, when the container answering was built from a
// different version than this CLI: the two speak an unversioned API and must be pinned
// together, so a stale container shows up as missing commands or odd behavior.
func checkVersion(daemon string) {
	if daemon == version.Version {
		return
	}
	versionOnce.Do(func() {
		running := "version " + daemon
		if daemon == "" {
			running = "a build that predates version reporting"
		}
		fix := "`greyman down`, rebuild the image, then `greyman up`"
		if version.IsRelease(version.Version) {
			fix = "`greyman down`, then `greyman up` (it offers to build a missing image)"
		}
		fmt.Fprintf(versionWarn, "warning: greyman is %s but the container runs %s; run %s\n", version.Version, running, fix)
	})
}

// checkReply runs checkVersion whenever the daemon answered, errors included: a
// container older than the CLI answers a newer command with "unknown route".
func checkReply(resp *client.Response, err error) {
	var apiErr *client.APIError
	switch {
	case err == nil:
		checkVersion(resp.Version)
	case errors.As(err, &apiErr):
		checkVersion(apiErr.Version)
	}
}

// explain replaces "connection refused" with the likely cause when the container is
// gone (reboot, Docker restart) and the saved state is stale.
func explain(ctx context.Context, err error) error {
	if errors.Is(err, client.ErrUnreachable) && docker.IsDown(ctx, docker.Name) {
		return state.ErrNotRunning
	}
	return err
}
