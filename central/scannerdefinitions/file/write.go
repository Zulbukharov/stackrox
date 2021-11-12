package file

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
)

// Write writes the contents of r into the path represented by the given file.
// The file's modified time is set to the given modifiedTime.
// Write is thread-safe.
func Write(file *Metadata, r io.Reader, modifiedTime time.Time) error {
	dir := filepath.Dir(file.GetPath())

	file.Lock()
	defer file.Unlock()

	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return errors.Wrap(err, "creating subdirectory for scanner defs")
	}
	scannerDefsFile, err := os.Create(file.GetPath())
	if err != nil {
		return errors.Wrap(err, "creating scanner defs file")
	}
	_, err = io.Copy(scannerDefsFile, r)
	if err != nil {
		return errors.Wrap(err, "copying scanner defs zip out")
	}
	err = os.Chtimes(file.GetPath(), time.Now(), modifiedTime)
	if err != nil {
		return errors.Wrap(err, "changing modified time of scanner defs")
	}

	file.SetLastModifiedTime(modifiedTime)

	return nil
}
