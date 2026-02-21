package files

import (
	"fmt"
	"strings"

	"github.com/fernandopn/benoit/tools"
)

func NewToolSet(root string) ([]tools.Tool, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("fs root is required")
	}

	sandboxFS, err := NewChrootFS(root)
	if err != nil {
		return nil, err
	}

	return []tools.Tool{
		NewListFilesToolWithFS(sandboxFS),
		NewGrepToolWithFS(sandboxFS),
		NewReadFileToolWithFS(sandboxFS),
		NewWriteFileToolWithFS(sandboxFS),
		NewPatchFileToolWithFS(sandboxFS),
	}, nil
}
