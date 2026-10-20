// Package cli implements the greyman subcommands.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"greyman/internal/client"
	"greyman/internal/docker"
	"greyman/internal/state"
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
var ungated = map[string]bool{"install": true, "down": true, "config": true}

func commands() []command {
	return []command{
		{"up", "greyman up [--headless | --watch] [--proxy URL] [--platform amd64] [--image NAME]", "start the browser container (--watch: live view URL)", cmdUp},
		{"down", "greyman down [--purge]", "stop the container (--purge also wipes cookies/profile)", cmdDown},
		{"status", "greyman status", "show container/browser state", cmdStatus},
		{"open", "greyman open <url> [--tab tN] [--snap]", "navigate the active tab", cmdOpen},
		{"snap", "greyman snap [--tab tN] [--max N]", "compact accessibility tree with refs (primary view)", cmdSnap},
		{"click", "greyman click <ref> [--tab tN] [--snap]", "click an element by ref", cmdClick},
		{"type", "greyman type <ref> <text> [--submit] [--tab tN] [--snap]", "type into an element (replaces content)", cmdType},
		{"scroll", "greyman scroll <up|down> [n] [--tab tN] [--snap]", "scroll n screens (default 1)", cmdScroll},
		{"back", "greyman back [--tab tN] [--snap]", "go back in history", cmdBack},
		{"read", "greyman read [--tab tN] [--max N]", "main page content as markdown (when snap is not enough)", cmdRead},
		{"shot", "greyman shot [ref] [-o path] [--full] [--tab tN]", "screenshot to a PNG file; prints the path", cmdShot},
		{"close", "greyman close [--tab tN]", "close a tab", cmdClose},
		{"set", "greyman set <option> <args> [--setconfig]", "change a setting on the running browser, e.g. geolocation (--setconfig also saves it)", cmdSet},
		{"config", "greyman config [list | get KEY | set KEY VALUE | unset KEY]", "show or change settings (applied on the next `up`)", cmdConfig},
		{"install", "greyman install [claude|codex|cursor|copilot|all|other [dir]] [--force]", "install the greyman skill for a coding agent (no name: detect)", cmdInstall},
	}
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

func (e *Env) help() {
	var b strings.Builder
	b.WriteString("greyman: a browser for coding agents\n\n")
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %-58s %s\n", c.usage, c.summary)
	}
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
	return resp, explain(ctx, err)
}

// explain replaces "connection refused" with the likely cause when the container is
// gone (reboot, Docker restart) and the saved state is stale.
func explain(ctx context.Context, err error) error {
	if errors.Is(err, client.ErrUnreachable) && docker.IsDown(ctx, docker.Name) {
		return state.ErrNotRunning
	}
	return err
}
