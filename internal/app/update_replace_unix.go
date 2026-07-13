//go:build !windows

package app

import "os"

func replaceExecutable(source, target string) error {
	return os.Rename(source, target)
}
