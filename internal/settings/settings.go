// Package settings is the registry of options users can tune. Each setting takes the
// first value found in: an `up` flag, the GREYMAN_<KEY> environment variable,
// options.cfg, then its default. The CLI resolves them all and hands the container
// settings to the daemon as environment variables.
package settings

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // so TZ validates on hosts without a zoneinfo database

	"greyman/internal/config"
	"greyman/internal/docker"
)

type Source string

const (
	Default Source = "default"
	File    Source = "file"
	Env     Source = "env"
	Flag    Source = "flag"
)

type Setting struct {
	Key string // options.cfg key; GREYMAN_<Key> in the shell overrides the file
	// Env is the variable the container sees ("" for settings only the CLI uses).
	Env string
	// Default is shown by `config list` and passed to the daemon like any other value.
	Default string
	// Detect, when set, supplies the default from the host instead of Default.
	Detect   func() string
	Validate func(string) error
	Doc      string
}

// All lists the settings in display order.
var All = []Setting{
	{Key: "NAV_GAP", Env: "GREYMAN_NAV_GAP", Default: "3000-6000", Validate: validRange,
		Doc: "min-max ms between navigations to one origin"},
	{Key: "ACT_GAP", Env: "GREYMAN_ACT_GAP", Default: "400-1200", Validate: validRange,
		Doc: "min-max ms between actions on one origin"},
	{Key: "ACTION_TIMEOUT_MS", Env: "GREYMAN_ACTION_TIMEOUT_MS", Default: "5000", Validate: validPositiveInt,
		Doc: "ms to wait for an element before an action fails"},
	{Key: "NAV_TIMEOUT_MS", Env: "GREYMAN_NAV_TIMEOUT_MS", Default: "30000", Validate: validPositiveInt,
		Doc: "ms to wait for a page to load before navigation fails"},
	{Key: "MAX_CHARS", Env: "GREYMAN_MAX_CHARS", Default: "24000", Validate: validPositiveInt,
		Doc: "default character limit for snap and read"},
	{Key: "SNAP_MAX_TEXT", Env: "GREYMAN_SNAP_MAX_TEXT", Default: "200", Validate: validPositiveInt,
		Doc: "snap clips any one text to this many characters; read is not clipped"},
	{Key: "PROXY", Env: "GREYMAN_PROXY", Validate: validProxy,
		Doc: "upstream proxy URL for browser traffic; may carry credentials"},
	{Key: "LOCALE", Env: "GREYMAN_LOCALE", Detect: hostLocale, Validate: validLocale,
		Doc: "browser locale such as en-US; default from LANG"},
	{Key: "TZ", Env: "TZ", Detect: hostTZ, Validate: validTZ,
		Doc: "browser time zone, an IANA name; default from the host"},
	{Key: "USE_GEOLOCATION", Env: "GREYMAN_USE_GEOLOCATION", Default: "0", Validate: validBool,
		Doc: "1 reports the coordinates below to pages that ask for the location; 0 denies every request"},
	{Key: "GEOLOCATION_COORD_LAT", Env: "GREYMAN_GEOLOCATION_COORD_LAT", Default: "0", Validate: validLatitude,
		Doc: "reported latitude, -90 to 90; used when USE_GEOLOCATION=1"},
	{Key: "GEOLOCATION_COORD_LON", Env: "GREYMAN_GEOLOCATION_COORD_LON", Default: "0", Validate: validLongitude,
		Doc: "reported longitude, -180 to 180; used when USE_GEOLOCATION=1"},
	{Key: "GEOLOCATION_ACCURACY", Env: "GREYMAN_GEOLOCATION_ACCURACY", Default: "10", Validate: validAccuracy,
		Doc: "reported accuracy radius in meters; used when USE_GEOLOCATION=1"},
	{Key: "HEADLESS", Env: "GREYMAN_HEADLESS", Default: "0", Validate: validBool,
		Doc: "1 runs Chromium headless instead of headed under Xvfb"},
	{Key: "WATCH", Env: "GREYMAN_WATCH", Default: "0", Validate: validBool,
		Doc: "1 serves a view-only live view on loopback; headed only"},
	{Key: "SCREEN_W", Env: "GREYMAN_SCREEN_W", Default: "1920", Validate: validPositiveInt,
		Doc: "browser window width in pixels"},
	{Key: "SCREEN_H", Env: "GREYMAN_SCREEN_H", Default: "1080", Validate: validPositiveInt,
		Doc: "browser window height in pixels"},
	{Key: "API_PORT", Default: "0", Validate: validPort,
		Doc: "host port for the daemon API on 127.0.0.1; 0 lets Docker pick"},
	{Key: "WATCH_PORT", Default: "0", Validate: validPort,
		Doc: "host port for the live view on 127.0.0.1; 0 lets Docker pick"},
	{Key: "IMAGE", Default: docker.DefaultImage, Validate: validNoSpace,
		Doc: "Docker image to run"},
	{Key: "PLATFORM", Validate: validPlatform,
		Doc: "container platform such as amd64; default is the image's own"},
}

// Lookup returns the setting registered under key.
func Lookup(key string) (Setting, bool) {
	for _, s := range All {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

// Display returns val as safe to print: the password in a proxy URL is masked, so
// credentials from options.cfg do not end up in a terminal or an agent's context.
func Display(key, val string) string {
	if key != "PROXY" {
		return val
	}
	scheme := ""
	if loc := schemeRE.FindStringIndex(val); loc != nil {
		scheme, val = val[:loc[1]], val[loc[1]:]
	}
	// The password may itself contain '/' or '@', so the userinfo is everything up to
	// the last '@', not up to the first '/'. This over-masks an '@' in a path; fine.
	at := strings.LastIndexByte(val, '@')
	if at < 0 {
		return scheme + val
	}
	name, _, hasPassword := strings.Cut(val[:at], ":")
	if !hasPassword {
		return scheme + val
	}
	return scheme + name + ":***" + val[at:]
}

// Value is a resolved setting.
type Value struct {
	Val    string
	Source Source
}

func (v Value) Bool() bool { return v.Val == "1" }

// Int is 0 for a value that is not a number, which Resolve never returns for a port.
func (v Value) Int() int { n, _ := strconv.Atoi(v.Val); return n }

// Resolve returns the effective value of every setting by key. flags holds the values
// given on the command line, also by key. Only the value that wins is validated, so a
// bad line in options.cfg that a flag overrides is not an error.
func Resolve(flags map[string]string) (map[string]Value, error) {
	file, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Value, len(All))
	for _, s := range All {
		v, err := s.resolve(flags, file)
		if err != nil {
			return nil, err
		}
		out[s.Key] = v
	}
	return out, nil
}

// Entry is one setting as Inspect reports it.
type Entry struct {
	Setting Setting
	Value   Value
	Err     error // the winning value is invalid; Value still holds it
}

// Inspect resolves every setting like Resolve, but reports an invalid value instead of
// failing, so `config list` can show what needs fixing.
func Inspect() ([]Entry, error) {
	file, err := config.Load()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(All))
	for i, s := range All {
		v, err := s.resolve(nil, file)
		entries[i] = Entry{s, v, err}
	}
	return entries, nil
}

// resolve picks the first non-empty value among flag, environment and file, else the
// default. If that value is invalid it returns it along with the error.
func (s Setting) resolve(flags, file map[string]string) (Value, error) {
	envName := "GREYMAN_" + s.Key
	layers := []struct {
		src   Source
		where string
		val   string
	}{
		{Flag, "command line", flags[s.Key]},
		{Env, "$" + envName, os.Getenv(envName)},
		{File, "options.cfg", file[s.Key]},
	}
	for _, l := range layers {
		if l.val == "" {
			continue
		}
		v := Value{l.val, l.src}
		if err := s.Validate(l.val); err != nil {
			return v, fmt.Errorf("invalid %s (%s): %w", s.Key, l.where, err)
		}
		return v, nil
	}
	if s.Detect != nil {
		return Value{s.Detect(), Default}, nil
	}
	return Value{s.Default, Default}, nil
}

// ContainerEnv returns NAME=value for every container setting that has a value, in
// registry order.
func ContainerEnv(vals map[string]Value) []string {
	var env []string
	for _, s := range All {
		if v := vals[s.Key].Val; s.Env != "" && v != "" {
			env = append(env, s.Env+"="+v)
		}
	}
	return env
}

// Warnings reports settings that look wrong without being invalid: host-detected
// defaults, and geolocation turned on with no coordinates. Detected values are not
// validated or rejected (the host is not the user's mistake), so `up` surfaces them.
// They come from the host, not a secret, so they are safe to print.
func Warnings(vals map[string]Value) []string {
	var out []string
	if vals["USE_GEOLOCATION"].Bool() &&
		vals["GEOLOCATION_COORD_LAT"].Source == Default && vals["GEOLOCATION_COORD_LON"].Source == Default {
		out = append(out, "USE_GEOLOCATION is on but no coordinates are set, so pages will see 0,0; set them with "+
			"`greyman config set GEOLOCATION_COORD_LAT <lat>` and `greyman config set GEOLOCATION_COORD_LON <lon>`")
	}
	for _, s := range All {
		v := vals[s.Key]
		if s.Detect == nil || v.Source != Default {
			continue
		}
		switch {
		case v.Val == "" && s.Key == "TZ":
			out = append(out, "could not detect the host time zone; the browser will use the container's (UTC). "+
				"Set one with `greyman config set TZ <IANA name>`")
		case v.Val != "" && s.Validate(v.Val) != nil:
			out = append(out, fmt.Sprintf("detected host %s %q is not valid; set one with `greyman config set %s <value>`",
				s.Key, v.Val, s.Key))
		}
	}
	return out
}

// Validators never echo the value: it may be a proxy URL with a password.

var (
	rangeRE  = regexp.MustCompile(`^(\d+)-(\d+)$`)
	localeRE = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
	schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
	platRE   = regexp.MustCompile(`^[a-z0-9]+(/[a-z0-9]+){0,2}$`)
	// Plain decimals only, so Go and the daemon's Number() agree on what a value means
	// (no hex, exponents, NaN or Inf).
	decimalRE = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// decimalIn validates a plain decimal number from lo to hi.
func decimalIn(v string, lo, hi float64, want string) error {
	if n, err := strconv.ParseFloat(v, 64); !decimalRE.MatchString(v) || err != nil || n < lo || n > hi {
		return errors.New(want)
	}
	return nil
}

func validLatitude(v string) error {
	return decimalIn(v, -90, 90, "want a latitude from -90 to 90, e.g. 37.7749")
}

func validLongitude(v string) error {
	return decimalIn(v, -180, 180, "want a longitude from -180 to 180, e.g. -122.4194")
}

func validAccuracy(v string) error {
	return decimalIn(v, 0, math.MaxFloat64, "want a radius in meters, 0 or more")
}

func validRange(v string) error {
	m := rangeRE.FindStringSubmatch(v)
	if m != nil {
		lo, err1 := strconv.Atoi(m[1])
		hi, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil && lo <= hi {
			return nil
		}
	}
	return errors.New("want MIN-MAX in milliseconds with MIN <= MAX, e.g. 3000-6000")
}

func validPositiveInt(v string) error {
	if n, err := strconv.Atoi(v); err != nil || n < 1 {
		return errors.New("want a positive integer")
	}
	return nil
}

func validPort(v string) error {
	if n, err := strconv.Atoi(v); err != nil || n < 0 || n > 65535 {
		return errors.New("want a port from 1 to 65535, or 0 to let Docker pick")
	}
	return nil
}

func validBool(v string) error {
	if v != "0" && v != "1" {
		return errors.New("want 0 or 1")
	}
	return nil
}

// validProxy accepts what the daemon's parseProxy does: a URL, or host:port for HTTP.
func validProxy(v string) error {
	if !schemeRE.MatchString(v) {
		v = "http://" + v
	}
	if u, err := url.Parse(v); err != nil || u.Hostname() == "" {
		return errors.New("not a valid proxy URL")
	}
	return nil
}

func validLocale(v string) error {
	if !localeRE.MatchString(v) {
		return errors.New("want a BCP 47 tag such as en-US")
	}
	return nil
}

func validTZ(v string) error {
	if _, err := time.LoadLocation(v); err != nil || v == "Local" {
		return errors.New("want an IANA time zone name such as America/Chicago")
	}
	return nil
}

func validNoSpace(v string) error {
	if strings.ContainsAny(v, " \t\r\n") {
		return errors.New("must not contain whitespace")
	}
	return nil
}

func validPlatform(v string) error {
	if !platRE.MatchString(v) {
		return errors.New("want a platform such as amd64 or linux/arm64")
	}
	return nil
}
