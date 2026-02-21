package files

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// GrepTool searches file contents using a regex pattern.
type GrepTool struct {
	fs FileSystem
}

func NewGrepTool() *GrepTool {
	return NewGrepToolWithFS(osFS{})
}

func NewGrepToolWithFS(fs FileSystem) *GrepTool {
	return &GrepTool{fs: fs}
}

func (g *GrepTool) Name() string {
	return "grep"
}

func (g *GrepTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        g.Name(),
			Description: openai.String("Search file contents by regex. Returns matching paths and line numbers."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					patternArgName: map[string]any{
						"type":        "string",
						"description": "Regex pattern to search",
					},
					pathArgName: map[string]any{
						"type":        "string",
						"description": "Search root path (optional)",
					},
					includeArgName: map[string]any{
						"type":        "string",
						"description": "Optional glob include filter (for example: *.go)",
					},
				},
				"required":             []string{patternArgName},
				"additionalProperties": false,
			},
			Strict: openai.Bool(false),
		},
	}
}

func (g *GrepTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	if err := requireFileSystem(g.fs); err != nil {
		return toolError(err), nil
	}

	pattern, err := requiredStringArg(args, patternArgName)
	if err != nil {
		return toolError(err), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return toolError(err), nil
	}
	searchRoot, err := optionalPathArg(args)
	if err != nil {
		return toolError(err), nil
	}
	rootAbs, err := absolutePathForFS(g.fs, searchRoot)
	if err != nil {
		return toolError(err), nil
	}

	include := ""
	if args != nil {
		if raw, ok := args[includeArgName]; ok {
			text, ok := raw.(string)
			if !ok {
				return toolError(fmt.Errorf("%s must be a string", includeArgName)), nil
			}
			include = strings.TrimSpace(text)
		}
	}
	var includeMatcher *globMatcher
	if include != "" {
		includeMatcher, err = newGlobMatcher(include)
		if err != nil {
			return toolError(err), nil
		}
	}

	matches := make([]string, 0)
	err = walkFS(g.fs, searchRoot, func(pathValue string, isDir bool) error {
		if isDir {
			return nil
		}
		absPath, err := absolutePathForFS(g.fs, pathValue)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootAbs, absPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if includeMatcher != nil && !includeMatcher.Match(rel) {
			return nil
		}
		data, err := g.fs.ReadFile(pathValue)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for idx, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d", absPath, idx+1))
			}
		}
		return nil
	})
	if err != nil {
		return toolError(err), nil
	}
	sort.Strings(matches)
	return strings.Join(matches, "\n"), nil
}
