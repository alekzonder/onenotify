// Package fsutil writes files so that readers never see a partial one.
package fsutil

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path so that readers see either the old or
// the new content, never a partial file: temp file in the same directory,
// then rename. The temp file's name starts with a dot.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return write(path, data, perm, false)
}

// WriteFileDurable is WriteFileAtomic that also survives a power loss: the
// file is synced before the rename and the directory after it. The queue uses
// it, because a message is written right before its source is deleted.
func WriteFileDurable(path string, data []byte, perm os.FileMode) error {
	return write(path, data, perm, true)
}

func write(path string, data []byte, perm os.FileMode, durable bool) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(perm)
	}
	if err == nil && durable {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
		return err
	}
	if durable {
		return SyncDir(dir)
	}
	return nil
}

// SyncDir flushes a directory, making renames and removals in it durable.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
