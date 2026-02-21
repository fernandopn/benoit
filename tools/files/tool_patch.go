package files

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

var updateHunkHeaderPattern = regexp.MustCompile(`^@@\s+-(\d+)(?:,\d+)?\s+\+\d+(?:,\d+)?\s+@@`)

const unifiedHunkHeaderFormat = "@@ -<start>[,<count>] +<start>[,<count>] @@"

// PatchFileTool applies file patches using a patch envelope.
type PatchFileTool struct {
	fs FileSystem
}

func NewPatchFileTool() *PatchFileTool {
	return NewPatchFileToolWithFS(osFS{})
}

func NewPatchFileToolWithFS(fs FileSystem) *PatchFileTool {
	return &PatchFileTool{fs: fs}
}

func (p *PatchFileTool) Name() string {
	return "apply_patch"
}

func (p *PatchFileTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name: p.Name(),
			Description: openai.String(
				"Apply a patch with *** Begin Patch / *** End Patch envelope and Add/Update/Delete file operations. Paths are sandbox paths, with / as the sandbox root.",
			),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					patchTextArgName: map[string]any{
						"type":        "string",
						"description": "Patch text containing Add/Update/Delete file operations",
					},
				},
				"required":             []string{patchTextArgName},
				"additionalProperties": false,
			},
			Strict: openai.Bool(true),
		},
	}
}

func (p *PatchFileTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	if err := requireFileSystem(p.fs); err != nil {
		return toolError(err), nil
	}

	patchText, err := requiredRawStringArg(args, patchTextArgName)
	if err != nil {
		return toolError(err), nil
	}
	if strings.TrimSpace(patchText) == "" {
		return toolError(errPatchTextRequired), nil
	}
	ops, err := parsePatchOperations(patchText)
	if err != nil {
		return toolError(err), nil
	}
	applied := 0
	for _, op := range ops {
		if err := p.applyOperation(op); err != nil {
			return toolError(err), nil
		}
		applied++
	}
	return fmt.Sprintf("applied patch with %d operation(s)", applied), nil
}

type patchOperation struct {
	kind      string
	path      string
	moveTo    string
	patchBody []string
}

func parsePatchOperations(patchText string) ([]patchOperation, error) {
	lines := strings.Split(patchText, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "*** Begin Patch" || strings.TrimSpace(lines[len(lines)-1]) != "*** End Patch" {
		return nil, fmt.Errorf("invalid patch envelope")
	}

	operations := make([]patchOperation, 0)
	for i := 1; i < len(lines)-1; {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			i++
			body := make([]string, 0)
			for i < len(lines)-1 && !strings.HasPrefix(lines[i], "*** ") {
				if !strings.HasPrefix(lines[i], "+") {
					return nil, fmt.Errorf("invalid add file line: %s", lines[i])
				}
				body = append(body, strings.TrimPrefix(lines[i], "+"))
				i++
			}
			operations = append(operations, patchOperation{kind: "add", path: path, patchBody: body})
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
			i++
			operations = append(operations, patchOperation{kind: "delete", path: path})
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			i++
			moveTo := ""
			if i < len(lines)-1 && strings.HasPrefix(lines[i], "*** Move to: ") {
				moveTo = strings.TrimSpace(strings.TrimPrefix(lines[i], "*** Move to: "))
				i++
			}
			body := make([]string, 0)
			for i < len(lines)-1 && !strings.HasPrefix(lines[i], "*** ") {
				body = append(body, lines[i])
				i++
			}
			operations = append(operations, patchOperation{kind: "update", path: path, moveTo: moveTo, patchBody: body})
		case strings.TrimSpace(line) == "":
			i++
		default:
			return nil, fmt.Errorf("invalid patch operation: %s", line)
		}
	}
	if len(operations) == 0 {
		return nil, fmt.Errorf("patch has no operations")
	}
	return operations, nil
}

func (p *PatchFileTool) applyOperation(op patchOperation) error {
	if strings.TrimSpace(op.path) == "" {
		return fmt.Errorf("operation path cannot be empty")
	}

	switch op.kind {
	case "add":
		content := strings.Join(op.patchBody, "\n")
		return p.fs.WriteFile(op.path, []byte(content))
	case "delete":
		return p.fs.RemoveFile(op.path)
	case "update":
		current, err := p.fs.ReadFile(op.path)
		if err != nil {
			return err
		}
		updated, err := applyUnifiedPatch(string(current), op.patchBody)
		if err != nil {
			return err
		}
		targetPath := op.path
		if strings.TrimSpace(op.moveTo) != "" {
			targetPath = op.moveTo
		}
		if err := p.fs.WriteFile(targetPath, []byte(updated)); err != nil {
			return err
		}
		if targetPath != op.path {
			if err := p.fs.RemoveFile(op.path); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported patch operation: %s", op.kind)
	}
}

type patchHunk struct {
	oldStart int
	lines    []string
}

func applyUnifiedPatch(original string, patchLines []string) (string, error) {
	hunks, err := parsePatchHunks(patchLines)
	if err != nil {
		return "", err
	}
	originalLines := strings.Split(original, "\n")
	output := make([]string, 0, len(originalLines)+len(patchLines))
	cursor := 0

	for _, hunk := range hunks {
		start := hunk.oldStart - 1
		if start < 0 || start > len(originalLines) {
			return "", fmt.Errorf("invalid hunk start line %d", hunk.oldStart)
		}
		if start < cursor {
			return "", fmt.Errorf("overlapping hunk at line %d", hunk.oldStart)
		}
		output = append(output, originalLines[cursor:start]...)
		cursor = start

		for _, line := range hunk.lines {
			if strings.HasPrefix(line, "\\ No newline at end of file") {
				continue
			}
			if line == "" {
				return "", fmt.Errorf("invalid empty patch line")
			}
			prefix := line[0]
			text := line[1:]
			switch prefix {
			case ' ':
				if cursor >= len(originalLines) || originalLines[cursor] != text {
					return "", fmt.Errorf("context mismatch while applying patch")
				}
				output = append(output, text)
				cursor++
			case '-':
				if cursor >= len(originalLines) || originalLines[cursor] != text {
					return "", fmt.Errorf("delete mismatch while applying patch")
				}
				cursor++
			case '+':
				output = append(output, text)
			default:
				return "", fmt.Errorf("invalid patch line prefix: %q", prefix)
			}
		}
	}

	output = append(output, originalLines[cursor:]...)
	return strings.Join(output, "\n"), nil
}

func parsePatchHunks(lines []string) ([]patchHunk, error) {
	hunks := make([]patchHunk, 0)
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		matches := updateHunkHeaderPattern.FindStringSubmatch(line)
		if len(matches) != 2 {
			return nil, fmt.Errorf("invalid hunk header at line %d: %q (expected format: %s)", i+1, line, unifiedHunkHeaderFormat)
		}
		oldStart := 0
		if _, err := fmt.Sscanf(matches[1], "%d", &oldStart); err != nil {
			return nil, fmt.Errorf("invalid hunk start at line %d", i+1)
		}
		i++
		hunkLines := make([]string, 0)
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
			hunkLines = append(hunkLines, lines[i])
			i++
		}
		hunks = append(hunks, patchHunk{oldStart: oldStart, lines: hunkLines})
	}
	if len(hunks) == 0 {
		return nil, fmt.Errorf("patch update operation has no hunks")
	}
	return hunks, nil
}
