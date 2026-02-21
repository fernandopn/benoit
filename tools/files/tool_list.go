package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ListFilesTool performs glob path matching.
type ListFilesTool struct {
	fs FileSystem
}

func NewListFilesTool() *ListFilesTool {
	return NewListFilesToolWithFS(osFS{})
}

func NewListFilesToolWithFS(fs FileSystem) *ListFilesTool {
	return &ListFilesTool{fs: fs}
}

func (l *ListFilesTool) Name() string {
	return "glob"
}

func (l *ListFilesTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        l.Name(),
			Description: openai.String("Match file paths using a glob pattern. Supports recursive ** patterns. Paths are resolved inside the filesystem sandbox."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					patternArgName: map[string]any{
						"type":        "string",
						"description": "Glob pattern such as **/*.go",
					},
					pathArgName: map[string]any{
						"type":        "string",
						"description": "Search root path (optional). Use sandbox paths, with / as the sandbox root.",
					},
				},
				"required":             []string{patternArgName},
				"additionalProperties": false,
			},
			Strict: openai.Bool(false),
		},
	}
}

func (l *ListFilesTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	if err := requireFileSystem(l.fs); err != nil {
		return toolError(err), nil
	}

	pattern, err := requiredStringArg(args, patternArgName)
	if err != nil {
		return toolError(err), nil
	}
	searchRoot, err := optionalPathArg(args)
	if err != nil {
		return toolError(err), nil
	}
	rootPath, err := absolutePathForFS(l.fs, searchRoot)
	if err != nil {
		return toolError(err), nil
	}

	matcher, err := newGlobMatcher(pattern)
	if err != nil {
		return toolError(err), nil
	}

	matches := make([]string, 0)
	err = walkFS(l.fs, searchRoot, func(pathValue string, isDir bool) error {
		_ = isDir
		absPath, err := absolutePathForFS(l.fs, pathValue)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootPath, absPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if matcher.Match(rel) {
			matches = append(matches, absPath)
		}
		return nil
	})
	if err != nil {
		err = mapGlobPathError(err, searchRoot)
		return toolError(err), nil
	}
	sort.Strings(matches)
	return strings.Join(matches, "\n"), nil
}

func mapGlobPathError(err error, requestedPath string) error {
	if !isPathNotFoundError(err) {
		return err
	}
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" || requestedPath == "." {
		requestedPath = "/"
	}
	return fmt.Errorf("path not found: %s (sandbox root is /); try path \"/\" with pattern \"*\"", requestedPath)
}

func isPathNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "no such file or directory") {
		return true
	}
	if strings.Contains(message, "path not found") {
		return true
	}
	return false
}

type globMatcher struct {
	regexps []*regexp.Regexp
}

func newGlobMatcher(pattern string) (*globMatcher, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("pattern cannot be empty")
	}
	patterns := expandBracePatterns(pattern)
	regexps := make([]*regexp.Regexp, 0, len(patterns))
	for _, candidate := range patterns {
		re, err := compilePathGlob(candidate)
		if err != nil {
			return nil, err
		}
		regexps = append(regexps, re)
	}
	return &globMatcher{regexps: regexps}, nil
}

func (m *globMatcher) Match(value string) bool {
	value = filepath.ToSlash(strings.TrimSpace(value))
	for _, re := range m.regexps {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

func compilePathGlob(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return nil, fmt.Errorf("glob pattern cannot be empty")
	}
	var out strings.Builder
	out.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		if ch == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					out.WriteString("(?:.*/)?")
					i += 2
					continue
				}
				out.WriteString(".*")
				i++
				continue
			}
			out.WriteString("[^/]*")
			continue
		}
		if ch == '?' {
			out.WriteString("[^/]")
			continue
		}
		out.WriteString(regexp.QuoteMeta(string(ch)))
	}
	out.WriteString("$")
	return regexp.Compile(out.String())
}

func expandBracePatterns(pattern string) []string {
	start := strings.Index(pattern, "{")
	if start < 0 {
		return []string{pattern}
	}
	end := strings.Index(pattern[start:], "}")
	if end < 0 {
		return []string{pattern}
	}
	end += start
	inside := pattern[start+1 : end]
	parts := strings.Split(inside, ",")
	if len(parts) == 0 {
		return []string{pattern}
	}
	prefix := pattern[:start]
	suffix := pattern[end+1:]
	expanded := make([]string, 0, len(parts))
	for _, part := range parts {
		expanded = append(expanded, prefix+strings.TrimSpace(part)+suffix)
	}
	return expanded
}

func absolutePathForFS(fs FileSystem, value string) (string, error) {
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	base, err := fs.Getwd()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" || value == "." {
		return filepath.Clean(base), nil
	}
	return filepath.Clean(filepath.Join(base, value)), nil
}

func walkFS(fs FileSystem, root string, visit func(pathValue string, isDir bool) error) error {
	entries, err := fs.ReadDir(root)
	if err != nil {
		if _, fileErr := fs.ReadFile(root); fileErr == nil {
			return visit(root, false)
		}
		return err
	}
	for _, entry := range entries {
		entryPath := filepath.Join(root, entry.Name())
		if err := visit(entryPath, entry.IsDir()); err != nil {
			return err
		}
		if entry.IsDir() {
			if err := walkFS(fs, entryPath, visit); err != nil {
				return err
			}
		}
	}
	return nil
}
