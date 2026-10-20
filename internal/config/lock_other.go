//go:build !unix

package config

// lockDir is a no-op where flock is unavailable; greyman targets macOS and Linux.
func lockDir(string) (func(), error) { return func() {}, nil }
