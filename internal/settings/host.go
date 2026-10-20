package settings

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// hostTZ returns the host's IANA timezone name so the browser's clock matches the
// egress IP's likely location. Empty if it can't be determined.
func hostTZ() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	if _, name, ok := strings.Cut(filepath.ToSlash(target), "zoneinfo/"); ok {
		return name
	}
	return ""
}

var hostLocaleRE = regexp.MustCompile(`^([a-z]{2,3})_([A-Z]{2})`)

// hostLocale maps LC_ALL/LANG like "en_US.UTF-8" to a BCP 47 tag "en-US".
func hostLocale() string {
	for _, k := range []string{"LC_ALL", "LANG"} {
		if m := hostLocaleRE.FindStringSubmatch(os.Getenv(k)); m != nil {
			return m[1] + "-" + m[2]
		}
	}
	return ""
}
