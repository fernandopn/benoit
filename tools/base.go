package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolKind classifies how a tool is exposed to a provider. It lets adapters
// make capability decisions explicitly instead of inferring them from a
// backend-specific wire format.
type ToolKind string

const (
	// ToolKindFunction is a locally dispatched function tool described by a
	// JSON Schema. It is supported by every backend.
	ToolKindFunction ToolKind = "function"
	// ToolKindHostedWebSearch is the provider-hosted web search tool. It is
	// only available on backends that run the tool server-side.
	ToolKindHostedWebSearch ToolKind = "hosted_web_search"
	// ToolKindHostedCodeInterpreter is the provider-hosted code interpreter
	// tool. It is only available on backends that run the tool server-side.
	ToolKindHostedCodeInterpreter ToolKind = "hosted_code_interpreter"
)

// ToolSchema is the provider-neutral description of a tool. It carries no
// backend SDK types so tool authors only declare a name, description, and JSON
// Schema. Provider packages translate it into their own wire format.
type ToolSchema struct {
	Name        string
	Description string
	// Parameters is the JSON Schema for the tool arguments. It is shared by all
	// supported provider APIs. It may be empty for tools that take no
	// arguments and for hosted tools.
	Parameters json.RawMessage
	// Kind selects how the tool is exposed to providers. The zero value is
	// invalid; tools must set it explicitly.
	Kind ToolKind
	// Strict requests strict JSON Schema adherence from the provider when it is
	// supported. It only applies to function tools.
	Strict bool
}

// Tool defines a callable tool that can be attached to model requests.
type Tool interface {
	Schema() ToolSchema
	Call(ctx context.Context, args map[string]any) (string, error)
}

// Validate checks the schema at registration time. It recovers the shape
// validation that the SDK types used to provide at compile time.
func (s ToolSchema) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	switch s.Kind {
	case ToolKindFunction, ToolKindHostedWebSearch, ToolKindHostedCodeInterpreter:
	default:
		return fmt.Errorf("tool %q has unknown kind: %q", s.Name, s.Kind)
	}
	if len(s.Parameters) > 0 {
		if _, err := s.ParametersMap(); err != nil {
			return fmt.Errorf("tool %q has invalid JSON Schema parameters: %w", s.Name, err)
		}
	}
	return nil
}

// ParametersMap decodes the JSON Schema parameters into a map. It returns a nil
// map when no parameters are set.
func (s ToolSchema) ParametersMap() (map[string]any, error) {
	if len(s.Parameters) == 0 {
		return nil, nil
	}
	var params map[string]any
	if err := json.Unmarshal(s.Parameters, &params); err != nil {
		return nil, err
	}
	return params, nil
}

// MustParameters marshals a JSON Schema map into a json.RawMessage. It panics
// only on a programming error (a schema that cannot be marshaled), mirroring
// the convention of regexp.MustCompile for static data.
func MustParameters(schema map[string]any) json.RawMessage {
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("tools: invalid tool parameters schema: %v", err))
	}
	return raw
}
