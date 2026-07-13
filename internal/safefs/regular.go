package safefs

import (
	"fmt"
	"strings"
)

func relativePathComponents(rel string) ([]string, error) {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return nil, fmt.Errorf("invalid relative path %q", rel)
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("invalid relative path %q", rel)
		}
	}
	return parts, nil
}
