// Package atomic writes files so a reader never sees a partial one.
package atomic

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to a uniquely named temp file beside path, sets its mode, then
// renames it into place. The temp file starts out 0600, so a secret is never briefly
// more widely readable, and it is removed on any failure.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
