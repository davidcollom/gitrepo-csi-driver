package materializer

import (
	"io/fs"
	"path/filepath"
)

func countFiles(root string) (int64, int64, error) {
	var files int64
	var size int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		size += info.Size()
		return nil
	})
	return files, size, err
}
