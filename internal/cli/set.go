package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"greyman/internal/config"
	"greyman/internal/settings"
)

// setPlan is one `set` option, parsed: what to send the daemon, and the options.cfg
// lines `--setconfig` writes once the daemon has applied it.
type setPlan struct {
	request map[string]any
	config  [][2]string
}

const geolocationUsage = "greyman set geolocation <lat> <lon> [accuracy] | off [--setconfig]"

func cmdSet(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman set <option> <args> [--setconfig]; options: " + geolocationUsage
	fs := newFlags("set", e, usage)
	save := fs.Bool("setconfig", false, "also save the change to options.cfg")
	pos, err := parseNumbers(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usageError{usage}
	}
	var plan setPlan
	switch pos[0] {
	case "geolocation":
		plan, err = planGeolocation(pos[1:])
	default:
		return fmt.Errorf("unknown option %q; options: geolocation", pos[0])
	}
	if err != nil {
		return err
	}

	// The live change goes first: if the browser is not running, or refuses it, the
	// config is left alone.
	resp, err := call(ctx, "set", plan.request)
	if err != nil {
		return err
	}
	line := resp.Text
	if *save {
		for _, kv := range plan.config {
			if err := config.Set(kv[0], kv[1]); err != nil {
				return fmt.Errorf("applied to the running browser, but saving %s failed: %w", kv[0], err)
			}
			// The environment outranks options.cfg, so the saved value would not apply on `up`.
			if os.Getenv("GREYMAN_"+kv[0]) != "" {
				fmt.Fprintf(e.Err, "warning: GREYMAN_%s is set in the environment and overrides the saved value\n", kv[0])
			}
		}
		line += " saved=options.cfg"
	}
	fmt.Fprintln(e.Out, line)
	return nil
}

// planGeolocation handles `<lat> <lon> [accuracy]` and `off`. The values are checked by
// the same validators as the config keys. A missing accuracy is left to the daemon,
// which uses the GEOLOCATION_ACCURACY it started with, and is not saved.
func planGeolocation(args []string) (setPlan, error) {
	if len(args) == 1 && args[0] == "off" {
		return setPlan{
			request: map[string]any{"option": "geolocation", "off": true},
			config:  [][2]string{{"USE_GEOLOCATION", "0"}},
		}, nil
	}
	if len(args) < 2 || len(args) > 3 {
		return setPlan{}, usageError{geolocationUsage}
	}
	fields := []struct{ name, key string }{
		{"latitude", "GEOLOCATION_COORD_LAT"},
		{"longitude", "GEOLOCATION_COORD_LON"},
		{"accuracy", "GEOLOCATION_ACCURACY"},
	}
	plan := setPlan{
		request: map[string]any{"option": "geolocation"},
		config:  [][2]string{{"USE_GEOLOCATION", "1"}},
	}
	for i, arg := range args {
		f := fields[i]
		s, _ := settings.Lookup(f.key)
		if err := s.Validate(arg); err != nil {
			return setPlan{}, fmt.Errorf("invalid %s: %w", f.name, err)
		}
		n, _ := strconv.ParseFloat(arg, 64)
		plan.request[f.name] = n
		plan.config = append(plan.config, [2]string{f.key, arg})
	}
	return plan, nil
}
