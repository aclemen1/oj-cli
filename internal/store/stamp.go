package store

import (
	"fmt"
	"io/fs"
	"path/filepath"
)

// Stamp is a cheap fingerprint of the store's files: it changes when a
// meeting, sitting or item file is written, added or removed, by any process.
func (s *Store) Stamp() (string, error) {
	var n int
	var latest int64
	var size int64
	err := filepath.WalkDir(s.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".jj", ".git", ".oj", "rendered":
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		n++
		size += info.Size()
		if t := info.ModTime().UnixNano(); t > latest {
			latest = t
		}
		return nil
	})
	return fmt.Sprintf("%d:%d:%d", n, size, latest), err
}
