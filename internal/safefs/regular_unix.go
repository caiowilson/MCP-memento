//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package safefs

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// OpenRegular opens rel beneath root without following any symlink component.
// O_NONBLOCK makes special files such as FIFOs safe to reject after fstat.
func OpenRegular(root, rel string) (*os.File, error) {
	parts, err := relativePathComponents(rel)
	if err != nil {
		return nil, err
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)

	dirFD := rootFD
	openedDirs := make([]int, 0, len(parts)-1)
	defer func() {
		for _, fd := range openedDirs {
			_ = unix.Close(fd)
		}
	}()
	for _, component := range parts[:len(parts)-1] {
		fd, openErr := unix.Openat(dirFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if openErr != nil {
			return nil, openErr
		}
		openedDirs = append(openedDirs, fd)
		dirFD = fd
	}

	fd, err := unix.Openat(dirFD, parts[len(parts)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), rel)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open regular path %q: invalid file descriptor", rel)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("path is not a regular file: %s", rel)
	}
	return file, nil
}
