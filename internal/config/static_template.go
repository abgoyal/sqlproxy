package config

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"text/template/parse"

	"sql-proxy/internal/tmpl"
)

// dynamicPathPrefixes are template paths only available at runtime execution.
var dynamicPathPrefixes = []string{
	".trigger",  // Request data: params, headers, cookies, path, method, etc.
	".steps",    // Step results
	".params",   // Computed step params
	".iter",     // Block iteration variables
	".workflow", // Workflow metadata: name, request_id, start_time
}

// StaticContext holds values available at config load time for template rendering.
type StaticContext struct {
	Vars map[string]string // variables.values from config
}

// staticFuncMap returns template functions safe for static evaluation.
// Excludes functions that depend on runtime context (like publicID/privateID).
func staticFuncMap() template.FuncMap {
	fm := tmpl.BaseFuncMap()
	// These functions are available at config load time
	// publicID/privateID are NOT included because they need the encoder
	// which isn't configured until after config loading
	return fm
}

// RenderStaticTemplate renders a template using only static context.
// Returns an error if the template references dynamic paths.
func RenderStaticTemplate(tmplStr string, ctx *StaticContext) (string, error) {
	if tmplStr == "" {
		return "", nil
	}

	// Check for any template syntax
	if !strings.Contains(tmplStr, "{{") {
		return tmplStr, nil
	}

	// Analyze paths first
	paths, err := ExtractTemplatePaths(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template syntax error: %w", err)
	}

	// Check for dynamic path references
	dynamicPaths := filterDynamicPaths(paths)
	if len(dynamicPaths) > 0 {
		return "", fmt.Errorf("template references dynamic path '%s' but this field must be resolvable at config load time", dynamicPaths[0])
	}

	// Build template data
	data := map[string]any{
		"vars": ctx.Vars,
	}

	// Parse and execute
	t, err := template.New("static").
		Funcs(staticFuncMap()).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}

	return buf.String(), nil
}

// ExtractTemplatePaths extracts all field paths referenced in a template.
// Returns paths like ".vars.X", ".trigger.params.id", etc.
func ExtractTemplatePaths(tmplStr string) ([]string, error) {
	t, err := template.New("analyze").
		Funcs(staticFuncMap()).
		Parse(tmplStr)
	if err != nil {
		return nil, err
	}

	paths := make(map[string]bool)
	if t.Tree != nil && t.Tree.Root != nil {
		extractPathsFromNode(t.Tree.Root, paths)
	}

	result := make([]string, 0, len(paths))
	for p := range paths {
		result = append(result, p)
	}
	return result, nil
}

// extractPathsFromNode recursively extracts field paths from a parse tree node.
func extractPathsFromNode(node parse.Node, paths map[string]bool) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				extractPathsFromNode(child, paths)
			}
		}
	case *parse.ActionNode:
		if n.Pipe != nil {
			extractPathsFromPipe(n.Pipe, paths)
		}
	case *parse.IfNode:
		if n.Pipe != nil {
			extractPathsFromPipe(n.Pipe, paths)
		}
		extractPathsFromNode(n.List, paths)
		extractPathsFromNode(n.ElseList, paths)
	case *parse.RangeNode:
		if n.Pipe != nil {
			extractPathsFromPipe(n.Pipe, paths)
		}
		extractPathsFromNode(n.List, paths)
		extractPathsFromNode(n.ElseList, paths)
	case *parse.WithNode:
		if n.Pipe != nil {
			extractPathsFromPipe(n.Pipe, paths)
		}
		extractPathsFromNode(n.List, paths)
		extractPathsFromNode(n.ElseList, paths)
	case *parse.TemplateNode:
		if n.Pipe != nil {
			extractPathsFromPipe(n.Pipe, paths)
		}
	}
}

// extractPathsFromPipe extracts field paths from a pipe node.
func extractPathsFromPipe(pipe *parse.PipeNode, paths map[string]bool) {
	if pipe == nil {
		return
	}

	for _, cmd := range pipe.Cmds {
		for _, arg := range cmd.Args {
			extractPathsFromArg(arg, paths)
		}
	}
}

// extractPathsFromArg extracts field paths from a command argument.
func extractPathsFromArg(arg parse.Node, paths map[string]bool) {
	switch n := arg.(type) {
	case *parse.FieldNode:
		// FieldNode represents .field.subfield access
		if len(n.Ident) > 0 {
			path := "." + strings.Join(n.Ident, ".")
			paths[path] = true
		}
	case *parse.ChainNode:
		// ChainNode represents a field chain on a value
		// The node itself is the value, Field contains the chain
		if n.Node != nil {
			extractPathsFromArg(n.Node, paths)
		}
	case *parse.PipeNode:
		extractPathsFromPipe(n, paths)
	case *parse.VariableNode:
		// Variables like $var - these are local to the template
		// We don't need to track these as they're defined within the template
	}
}

// filterDynamicPaths returns paths that reference dynamic runtime context.
func filterDynamicPaths(paths []string) []string {
	var dynamic []string
	for _, path := range paths {
		for _, prefix := range dynamicPathPrefixes {
			if strings.HasPrefix(path, prefix) {
				dynamic = append(dynamic, path)
				break
			}
		}
	}
	return dynamic
}
