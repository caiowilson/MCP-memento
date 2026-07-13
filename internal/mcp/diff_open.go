package mcp

import (
	"os"

	"memento-mcp/internal/safefs"
)

func openDiffRegularFile(root, rel string) (*os.File, error) {
	return safefs.OpenRegular(root, rel)
}
