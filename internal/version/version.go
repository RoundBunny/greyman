// Package version holds the build version, stamped with
// -ldflags "-X greyman/internal/version.Version=v1.2.3".
package version

import "regexp"

var Version = "dev"

var releaseRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-rc\.\d+)?$`)

// IsRelease reports whether v is a clean release tag (v0.1.0, v0.1.0-rc.1) rather than a
// dev build (dev, a commit hash, anything -dirty).
func IsRelease(v string) bool { return releaseRE.MatchString(v) }
