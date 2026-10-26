//go:build !darwin && !linux

package cli

import "os"

// isTerminal is always false where greyman has no termios check; greyman targets macOS
// and Linux.
func isTerminal(*os.File) bool { return false }
