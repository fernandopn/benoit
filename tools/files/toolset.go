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

	restrictedFS, err := NewRestrictedFS(root)
	if err != nil {
		return nil, err
	}

	return []tools.Tool{
		NewListFilesToolWithFS(restrictedFS),
		NewCurrentDirectoryToolWithFS(restrictedFS),
		NewReadFileToolWithFS(restrictedFS),
	}, nil
}
