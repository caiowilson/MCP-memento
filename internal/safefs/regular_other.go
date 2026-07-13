//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package safefs

import (
	"fmt"
	"os"
	"strings"
)

// OpenRegular uses os.Root containment on platforms without the Unix openat
// implementation and rejects path-identity changes across validation/open.
func OpenRegular(rootPath, rel string) (*os.File, error) {
	parts, err := relativePathComponents(rel)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	var expected os.FileInfo
	for index := range parts {
		prefix := strings.Join(parts[:index+1], "/")
		info, statErr := root.Lstat(prefix)
		if statErr != nil {
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("path contains a symbolic link: %s", rel)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("path component is not a directory: %s", prefix)
		}
		expected = info
	}
	if expected == nil || !expected.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file: %s", rel)
	}

	file, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(expected, openedInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("path changed during secure open: %s", rel)
	}
	return file, nil
}
