package main

import (
	"os"
	"path/filepath"

	"github.com/davidcollom/gitrepo-csi-driver/pkg/materializer"
)

func publishReadOnly(source, target string) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := materializer.CopyContentTree(source, target); err != nil {
		return err
	}
	return chmodReadOnly(target)
}

func unpublish(target string) error {
	return os.RemoveAll(target)
}

func chmodReadOnly(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode.Type() == os.ModeSymlink {
			return nil
		}
		if d.IsDir() {
			return os.Chmod(path, 0o555) // #nosec G302,G122 -- published volume directories are intentionally readonly and traversable by workload users.
		}
		if mode.IsRegular() {
			return os.Chmod(path, mode.Perm()&0o555) // #nosec G122 -- WalkDir does not follow symlinks; regular files are made readonly for the published copy.
		}
		return nil
	})
}
