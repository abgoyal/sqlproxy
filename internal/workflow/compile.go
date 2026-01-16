package workflow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"text/template"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
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

// TemplateFuncs provides template functions for workflow templates.
// Note: Error handling in these functions returns error strings rather than Go errors
// because text/template FuncMap functions cannot propagate errors cleanly.
var TemplateFuncs = template.FuncMap{
	// JSON functions
	"json": func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("[json error: %v]", err)
		}
		return string(b)
	},
	"jsonIndent": func(v any) string {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("[json error: %v]", err)
		}
		return string(b)
	},

	// String functions
	"upper":     strings.ToUpper,
	"lower":     strings.ToLower,
	"trim":      strings.TrimSpace,
	"replace":   strings.ReplaceAll,
	"contains":  strings.Contains,
	"hasPrefix": strings.HasPrefix,
	"hasSuffix": strings.HasSuffix,

	// Default value functions
	"default": func(def, val any) any {
		if val == nil {
			return def
		}
		// Use reflect for comprehensive zero-value checking
		rv := reflect.ValueOf(val)
		if rv.IsZero() {
			return def
		}
		return val
	},
	"coalesce": func(vals ...string) string {
		for _, v := range vals {
			if v != "" {
				return v
			}
		}
		return ""
	},
	"getOr": func(m any, key string, fallback string) string {
		val, found := getMapValue(m, key)
		if !found || val == "" {
			return fallback
		}
		return val
	},

	// Map/array access functions
	"require": func(m any, key string) (string, error) {
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
	},
	"has": func(m any, key string) bool {
		val, found := getMapValue(m, key)
		return found && val != ""
	},

	// Math functions
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"mul": func(a, b int) int { return a * b },
	"div": func(a, b int) (int, error) {
		if b == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return a / b, nil
	},
	"mod": func(a, b int) (int, error) {
		if b == 0 {
			return 0, fmt.Errorf("modulo by zero")
		}
		return a % b, nil
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
// - Direct expression: "steps.x.count > 0" -> compiles directly
func resolveCondition(cond string, aliases map[string]*CompiledCondition) (*vm.Program, error) {
	// Check for direct alias reference
	if cc, ok := aliases[cond]; ok {
		return cc.Prog, nil
	}

	// Check for negated alias (e.g., "!found")
	if strings.HasPrefix(cond, "!") {
		aliasName := strings.TrimPrefix(cond, "!")
		if cc, ok := aliases[aliasName]; ok {
			// Wrap the source expression in !() and compile
			return compileCondition("!(" + cc.Source + ")")
		}
	}

	// Fall back to compiling as a direct expression
	return compileCondition(cond)
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
