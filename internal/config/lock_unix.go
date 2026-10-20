//go:build unix

package config

import (
	"os"
	"syscall"
)

// lockDir takes an exclusive advisory lock on the directory itself, so no lock file is
// left behind. The lock is released when unlock runs or the process exits.
func lockDir(dir string) (unlock func(), err error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil // closing the descriptor drops the lock
}
