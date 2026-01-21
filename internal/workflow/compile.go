package workflow

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync/atomic"
	"text/template"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"sql-proxy/internal/tmpl"
)

// CompiledCondition holds a condition alias with its source expression and compiled program.
type CompiledCondition struct {
	Source string      // Original expression string (e.g., "steps.fetch.count > 0")
	Prog   *vm.Program // Compiled program
}

// CompiledWorkflow holds a workflow with pre-compiled expressions and templates.
type CompiledWorkflow struct {
	Config     *WorkflowConfig
	Conditions map[string]*CompiledCondition // Named condition aliases
	Triggers   []*CompiledTrigger
	Steps      []*CompiledStep
}

// CompiledTrigger holds a trigger with pre-compiled templates.
type CompiledTrigger struct {
	Config     *TriggerConfig
	CacheKey   *template.Template // For HTTP triggers with caching
	RateLimits []*CompiledRateLimit
}

// CompiledRateLimit holds a rate limit with pre-compiled key template.
type CompiledRateLimit struct {
	Config  *RateLimitRefConfig
	KeyTmpl *template.Template
}

// CompiledStep holds a step with pre-compiled condition and templates.
type CompiledStep struct {
	Config    *StepConfig
	Condition *vm.Program // Compiled condition expression
	Index     int         // Step index in workflow

	// Cache key template (for query and httpcall steps)
	CacheKeyTmpl *template.Template

	// Computed params templates (available for all step types)
	ParamTmpls map[string]*template.Template

	// Query step templates
	SQLTmpl *template.Template

	// HTTPCall step templates
	URLTmpl     *template.Template
	BodyTmpl    *template.Template
	HeaderTmpls map[string]*template.Template

	// Response step templates
	TemplateTmpl *template.Template

	// Block step (nested steps)
	Iterate    *CompiledIterate
	BlockSteps []*CompiledStep
}

// CompiledIterate holds compiled iteration config.
type CompiledIterate struct {
	Config   *IterateConfig
	OverExpr *vm.Program // Expression to evaluate the collection
}

// getMapValue extracts a string value from various map types.
// Returns the value and whether it was found.
func getMapValue(m any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	switch typed := m.(type) {
	case map[string]string:
		v, ok := typed[key]
		return v, ok
	case map[string]any:
		v, ok := typed[key]
		if !ok {
			return "", false
		}
		if s, ok := v.(string); ok {
			return s, true
		}
		return "", false
	}
	return "", false
}

// templateEncoder holds the encoder for publicID/privateID functions.
// Uses atomic.Value for thread-safe access in case of future config reload.
// Currently set once at startup, but atomic ensures safe concurrent reads.
var templateEncoder atomic.Value

// PublicIDEncoder provides public ID encoding/decoding for templates.
type PublicIDEncoder interface {
	Encode(namespace string, id int64) (string, error)
	Decode(namespace, publicID string) (int64, error)
}

// encoderWrapper wraps an encoder to allow storing nil via atomic.Value.
// atomic.Value doesn't support storing nil directly.
type encoderWrapper struct {
	enc PublicIDEncoder
}

// SetTemplateEncoder sets the encoder for publicID/privateID template functions.
// Thread-safe via atomic.Value. Pass nil to clear the encoder.
func SetTemplateEncoder(enc PublicIDEncoder) {
	templateEncoder.Store(encoderWrapper{enc: enc})
}

// getTemplateEncoder returns the current encoder, or nil if not set.
func getTemplateEncoder() PublicIDEncoder {
	v := templateEncoder.Load()
	if v == nil {
		return nil
	}
	return v.(encoderWrapper).enc
}

// toInt64 converts various numeric types to int64.
func toInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case uint64:
		// Check for overflow - uint64 values > MaxInt64 cannot be represented
		if n > math.MaxInt64 {
			return 0, fmt.Errorf("uint64 value %d exceeds int64 max", n)
		}
		return int64(n), nil
	case float64:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("invalid numeric type %T", v)
	}
}

// TemplateFuncs provides template functions for workflow templates.
// Built from tmpl.BaseFuncMap() with workflow-specific overrides for map[string]any handling.
// Note: publicID/privateID require an encoder to be set via SetTemplateEncoder.
var TemplateFuncs template.FuncMap

func init() {
	// Start with all functions from tmpl.BaseFuncMap()
	TemplateFuncs = tmpl.BaseFuncMap()

	// Override functions that need to handle map[string]any (workflow context uses any types)
	TemplateFuncs["getOr"] = func(m any, key string, fallback string) string {
		val, found := getMapValue(m, key)
		if !found || val == "" {
			return fallback
		}
		return val
	}
	TemplateFuncs["require"] = func(m any, key string) (string, error) {
		if m == nil {
			return "", fmt.Errorf("required key %q: nil map", key)
		}
		val, found := getMapValue(m, key)
		if !found {
			return "", fmt.Errorf("required key %q not found", key)
		}
		if val == "" {
			return "", fmt.Errorf("required key %q is empty", key)
		}
		return val, nil
	}
	TemplateFuncs["has"] = func(m any, key string) bool {
		val, found := getMapValue(m, key)
		return found && val != ""
	}

	// Add encoder functions (require SetTemplateEncoder to be called)
	TemplateFuncs["publicID"] = func(namespace string, id any) (string, error) {
		enc := getTemplateEncoder()
		if enc == nil {
			return "", fmt.Errorf("publicID: encoder not configured")
		}
		idVal, err := toInt64(id)
		if err != nil {
			return "", fmt.Errorf("publicID: %w", err)
		}
		return enc.Encode(namespace, idVal)
	}
	TemplateFuncs["privateID"] = func(namespace string, publicID string) (int64, error) {
		enc := getTemplateEncoder()
		if enc == nil {
			return 0, fmt.Errorf("privateID: encoder not configured")
		}
		return enc.Decode(namespace, publicID)
	}
}

// exprFuncs contains custom functions for expr evaluation in conditions.
// Defined once at package level, merged into expression environments.
var exprFuncs = map[string]any{
	// isValidPublicID checks if a public ID is valid for the given namespace.
	// Usage in conditions: isValidPublicID("task", trigger.params.public_id)
	"isValidPublicID": func(namespace string, publicID any) bool {
		enc := getTemplateEncoder()
		if enc == nil {
			return false
		}
		pidStr, ok := publicID.(string)
		if !ok {
			return false
		}
		_, err := enc.Decode(namespace, pidStr)
		return err == nil
	},
}

// Compile compiles a workflow configuration into an executable form.
func Compile(cfg *WorkflowConfig) (*CompiledWorkflow, error) {
	cw := &CompiledWorkflow{
		Config:     cfg,
		Conditions: make(map[string]*CompiledCondition),
	}

	// Compile named condition aliases
	for name, condExpr := range cfg.Conditions {
		prog, err := compileCondition(condExpr)
		if err != nil {
			return nil, fmt.Errorf("condition '%s': %w", name, err)
		}
		cw.Conditions[name] = &CompiledCondition{
			Source: condExpr,
			Prog:   prog,
		}
	}

	// Compile triggers
	for i, trigCfg := range cfg.Triggers {
		ct, err := compileTrigger(&trigCfg)
		if err != nil {
			return nil, fmt.Errorf("triggers[%d]: %w", i, err)
		}
		cw.Triggers = append(cw.Triggers, ct)
	}

	// Compile steps
	for i, stepCfg := range cfg.Steps {
		cs, err := compileStep(&stepCfg, i, cw.Conditions)
		if err != nil {
			name := stepCfg.Name
			if name == "" {
				name = fmt.Sprintf("#%d", i)
			}
			return nil, fmt.Errorf("steps[%s]: %w", name, err)
		}
		cw.Steps = append(cw.Steps, cs)
	}

	return cw, nil
}

func compileTrigger(cfg *TriggerConfig) (*CompiledTrigger, error) {
	ct := &CompiledTrigger{Config: cfg}

	// Compile cache key template
	if cfg.Cache != nil && cfg.Cache.Key != "" {
		tmpl, err := template.New("cache_key").Funcs(TemplateFuncs).Parse(cfg.Cache.Key)
		if err != nil {
			return nil, fmt.Errorf("cache.key template: %w", err)
		}
		ct.CacheKey = tmpl
	}

	// Compile rate limit key templates
	for i, rl := range cfg.RateLimit {
		crl := &CompiledRateLimit{Config: &cfg.RateLimit[i]}
		if rl.Key != "" {
			tmpl, err := template.New("rate_limit_key").Funcs(TemplateFuncs).Parse(rl.Key)
			if err != nil {
				return nil, fmt.Errorf("rate_limit[%d].key template: %w", i, err)
			}
			crl.KeyTmpl = tmpl
		}
		ct.RateLimits = append(ct.RateLimits, crl)
	}

	return ct, nil
}

func compileStep(cfg *StepConfig, index int, aliases map[string]*CompiledCondition) (*CompiledStep, error) {
	cs := &CompiledStep{
		Config: cfg,
		Index:  index,
	}

	// Compile condition if present
	if cfg.Condition != "" {
		prog, err := resolveCondition(cfg.Condition, aliases)
		if err != nil {
			return nil, fmt.Errorf("condition: %w", err)
		}
		cs.Condition = prog
	}

	// Compile cache key template if present (for query and httpcall steps)
	if cfg.Cache != nil && cfg.Cache.Key != "" {
		tmpl, err := template.New("cache_key").Funcs(TemplateFuncs).Parse(cfg.Cache.Key)
		if err != nil {
			return nil, fmt.Errorf("cache.key template: %w", err)
		}
		cs.CacheKeyTmpl = tmpl
	}

	// Compile params templates if present (available for all step types)
	if len(cfg.Params) > 0 {
		cs.ParamTmpls = make(map[string]*template.Template)
		for name, val := range cfg.Params {
			tmpl, err := template.New("param_" + name).Funcs(TemplateFuncs).Parse(val)
			if err != nil {
				return nil, fmt.Errorf("params[%s] template: %w", name, err)
			}
			cs.ParamTmpls[name] = tmpl
		}
	}

	// Compile type-specific templates
	switch cfg.StepType() {
	case "query":
		if cfg.SQL != "" {
			tmpl, err := template.New("sql").Funcs(TemplateFuncs).Parse(cfg.SQL)
			if err != nil {
				return nil, fmt.Errorf("sql template: %w", err)
			}
			cs.SQLTmpl = tmpl
		}

	case "httpcall":
		if cfg.URL != "" {
			tmpl, err := template.New("url").Funcs(TemplateFuncs).Parse(cfg.URL)
			if err != nil {
				return nil, fmt.Errorf("url template: %w", err)
			}
			cs.URLTmpl = tmpl
		}
		if cfg.Body != "" {
			tmpl, err := template.New("body").Funcs(TemplateFuncs).Parse(cfg.Body)
			if err != nil {
				return nil, fmt.Errorf("body template: %w", err)
			}
			cs.BodyTmpl = tmpl
		}
		if len(cfg.Headers) > 0 {
			cs.HeaderTmpls = make(map[string]*template.Template)
			for name, val := range cfg.Headers {
				tmpl, err := template.New("header_" + name).Funcs(TemplateFuncs).Parse(val)
				if err != nil {
					return nil, fmt.Errorf("headers[%s] template: %w", name, err)
				}
				cs.HeaderTmpls[name] = tmpl
			}
		}

	case "response":
		if cfg.Template != "" {
			tmpl, err := template.New("response").Funcs(TemplateFuncs).Parse(cfg.Template)
			if err != nil {
				return nil, fmt.Errorf("template: %w", err)
			}
			cs.TemplateTmpl = tmpl
		}
		if len(cfg.Headers) > 0 {
			cs.HeaderTmpls = make(map[string]*template.Template)
			for name, val := range cfg.Headers {
				tmpl, err := template.New("header_" + name).Funcs(TemplateFuncs).Parse(val)
				if err != nil {
					return nil, fmt.Errorf("headers[%s] template: %w", name, err)
				}
				cs.HeaderTmpls[name] = tmpl
			}
		}

	case "block":
		// Compile iterate expression
		if cfg.Iterate != nil {
			ci := &CompiledIterate{Config: cfg.Iterate}
			if cfg.Iterate.Over != "" {
				prog, err := compileExpression(cfg.Iterate.Over)
				if err != nil {
					return nil, fmt.Errorf("iterate.over: %w", err)
				}
				ci.OverExpr = prog
			}
			cs.Iterate = ci
		}

		// Compile nested steps
		for i, nestedCfg := range cfg.Steps {
			nested, err := compileStep(&nestedCfg, i, aliases)
			if err != nil {
				name := nestedCfg.Name
				if name == "" {
					name = fmt.Sprintf("#%d", i)
				}
				return nil, fmt.Errorf("steps[%s]: %w", name, err)
			}
			cs.BlockSteps = append(cs.BlockSteps, nested)
		}
	}

	return cs, nil
}

// resolveCondition resolves a condition string to a compiled program.
// Supports:
// - Direct alias reference: "found" -> uses pre-compiled alias
// - Negated alias: "!found" -> wraps alias expression in !()
// - Compound expressions: "valid_id && found" -> expands aliases inline
// - Direct expression: "steps.x.count > 0" -> compiles directly
func resolveCondition(cond string, aliases map[string]*CompiledCondition) (*vm.Program, error) {
	// Check for direct alias reference (optimization: reuse pre-compiled program)
	if cc, ok := aliases[cond]; ok {
		return cc.Prog, nil
	}

	// Check for simple negated alias (e.g., "!found")
	if strings.HasPrefix(cond, "!") {
		aliasName := strings.TrimPrefix(cond, "!")
		if cc, ok := aliases[aliasName]; ok {
			return compileCondition("!(" + cc.Source + ")")
		}
	}

	// Expand all alias references in compound expressions
	expanded := expandAliases(cond, aliases)

	return compileCondition(expanded)
}

// expandAliases replaces alias references in an expression with their source expressions.
// Handles word boundaries to avoid replacing property access (e.g., "found" won't match "steps.found").
func expandAliases(expr string, aliases map[string]*CompiledCondition) string {
	if len(aliases) == 0 {
		return expr
	}

	result := expr
	for name, cc := range aliases {
		// Build regex to match alias as a standalone identifier (not part of a dotted path)
		// Must NOT be preceded by a dot (property access) or alphanumeric
		// Can be preceded by: start, space, operators (&&, ||, !), parentheses
		// Examples matched: "found", "!found", "x && found", "(found)", "found || y"
		// Examples NOT matched: "steps.found", "not_found"
		pattern := regexp.MustCompile(`(^|[^a-zA-Z0-9_.])` + regexp.QuoteMeta(name) + `([^a-zA-Z0-9_]|$)`)
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Preserve the boundary characters and wrap the source in parentheses
			prefix := ""
			suffix := ""
			if len(match) > len(name) {
				if match[0] != name[0] {
					prefix = string(match[0])
				}
				if match[len(match)-1] != name[len(name)-1] {
					suffix = string(match[len(match)-1])
				}
			}
			return prefix + "(" + cc.Source + ")" + suffix
		})
	}
	return result
}

func compileCondition(exprStr string) (*vm.Program, error) {
	return compileExprWithType(exprStr, true)
}

func compileExpression(exprStr string) (*vm.Program, error) {
	return compileExprWithType(exprStr, false)
}

func compileExprWithType(exprStr string, asBool bool) (*vm.Program, error) {
	opts := []expr.Option{
		expr.AllowUndefinedVariables(), // Allow forward references to be checked at runtime
	}
	if asBool {
		opts = append(opts, expr.AsBool())
	}
	return expr.Compile(exprStr, opts...)
}

// EvalCondition evaluates a compiled condition against an environment.
func EvalCondition(prog *vm.Program, env map[string]any) (bool, error) {
	result, err := vm.Run(prog, env)
	if err != nil {
		return false, err
	}
	if b, ok := result.(bool); ok {
		return b, nil
	}
	return false, fmt.Errorf("condition did not return bool: %T", result)
}

// EvalExpression evaluates a compiled expression against an environment.
func EvalExpression(prog *vm.Program, env map[string]any) (any, error) {
	return vm.Run(prog, env)
}
