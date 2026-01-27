package workflow

import (
	"fmt"
	"math"
	"sync/atomic"
	"text/template"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"

	"sql-proxy/internal/tmpl"
)

// CompiledCondition holds a condition alias with its source expression.
// The source is stored for debugging and error messages. At compile time,
// aliases are expanded into step conditions via AST patching.
type CompiledCondition struct {
	Source string // Original expression string (e.g., "steps.fetch.count > 0")
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
// Built from tmpl.ExprFuncs() with workflow-specific additions.
// Defined once at package level, merged into expression environments.
var exprFuncs map[string]any

func init() {
	// Start with common functions from tmpl package
	exprFuncs = tmpl.ExprFuncs()

	// Add workflow-specific functions
	// isValidPublicID checks if a public ID is valid for the given namespace.
	// Usage in conditions: isValidPublicID("task", trigger.params.public_id)
	exprFuncs["isValidPublicID"] = func(namespace string, publicID any) bool {
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
	}
}

// Compile compiles a workflow configuration into an executable form.
func Compile(cfg *WorkflowConfig) (*CompiledWorkflow, error) {
	cw := &CompiledWorkflow{
		Config:     cfg,
		Conditions: make(map[string]*CompiledCondition),
	}

	// Build alias ASTs in dependency order (handles aliases referencing other aliases)
	var aliasASTs map[string]ast.Node
	if len(cfg.Conditions) > 0 {
		order, err := topoSortAliases(cfg.Conditions)
		if err != nil {
			return nil, fmt.Errorf("conditions: %w", err)
		}

		aliasASTs, err = buildAliasASTs(cfg.Conditions, order)
		if err != nil {
			return nil, fmt.Errorf("conditions: %w", err)
		}

		// Store source expressions for debugging/error messages
		for name, source := range cfg.Conditions {
			cw.Conditions[name] = &CompiledCondition{Source: source}
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
		cs, err := compileStep(&stepCfg, i, aliasASTs)
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

func compileStep(cfg *StepConfig, index int, aliasASTs map[string]ast.Node) (*CompiledStep, error) {
	cs := &CompiledStep{
		Config: cfg,
		Index:  index,
	}

	// Compile condition if present (with alias expansion via AST patching)
	if cfg.Condition != "" {
		prog, err := compileConditionWithAliases(cfg.Condition, aliasASTs)
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
			nested, err := compileStep(&nestedCfg, i, aliasASTs)
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

// aliasPatcher implements ast.Visitor to replace alias identifiers with their ASTs.
type aliasPatcher struct {
	aliases map[string]ast.Node // alias name -> parsed AST
}

// Visit implements ast.Visitor. It replaces identifier nodes that match alias names.
func (p *aliasPatcher) Visit(node *ast.Node) {
	if node == nil || *node == nil {
		return
	}
	ident, ok := (*node).(*ast.IdentifierNode)
	if !ok {
		return
	}
	if replacement, exists := p.aliases[ident.Value]; exists {
		ast.Patch(node, replacement)
	}
}

// compileConditionWithAliases compiles a condition, expanding alias references via AST patching.
func compileConditionWithAliases(exprStr string, aliasASTs map[string]ast.Node) (*vm.Program, error) {
	opts := []expr.Option{
		expr.AllowUndefinedVariables(),
		expr.AsBool(),
	}
	if len(aliasASTs) > 0 {
		opts = append(opts, expr.Patch(&aliasPatcher{aliases: aliasASTs}))
	}
	return expr.Compile(exprStr, opts...)
}

// aliasRefFinder is a visitor that collects alias references from an AST.
type aliasRefFinder struct {
	aliasNames map[string]bool
	refs       []string
	seen       map[string]bool
}

func (f *aliasRefFinder) Visit(node *ast.Node) {
	if node == nil || *node == nil {
		return
	}
	if ident, ok := (*node).(*ast.IdentifierNode); ok {
		if f.aliasNames[ident.Value] && !f.seen[ident.Value] {
			f.refs = append(f.refs, ident.Value)
			f.seen[ident.Value] = true
		}
	}
}

// extractAliasRefs finds alias names referenced in an expression.
// Returns a list of alias names that appear as standalone identifiers.
func extractAliasRefs(exprStr string, aliasNames map[string]bool) ([]string, error) {
	tree, err := parser.Parse(exprStr)
	if err != nil {
		return nil, err
	}

	finder := &aliasRefFinder{
		aliasNames: aliasNames,
		seen:       make(map[string]bool),
	}
	ast.Walk(&tree.Node, finder)
	return finder.refs, nil
}

// topoSortAliases returns aliases in dependency order (dependencies first).
// Returns an error if a circular dependency is detected.
func topoSortAliases(aliases map[string]string) ([]string, error) {
	if len(aliases) == 0 {
		return nil, nil
	}

	aliasNames := make(map[string]bool, len(aliases))
	for name := range aliases {
		aliasNames[name] = true
	}

	// Build dependency graph: deps[A] = [B, C] means A depends on B and C
	deps := make(map[string][]string)
	for name, source := range aliases {
		refs, err := extractAliasRefs(source, aliasNames)
		if err != nil {
			return nil, fmt.Errorf("alias %q: %w", name, err)
		}
		deps[name] = refs
	}

	// Kahn's algorithm for topological sort
	// Count incoming edges (how many aliases depend on each alias)
	inDegree := make(map[string]int)
	for name := range aliases {
		inDegree[name] = 0
	}
	for _, depList := range deps {
		for _, dep := range depList {
			inDegree[dep]++
		}
	}

	// Start with aliases that have no dependents
	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		// Pop from queue
		curr := queue[0]
		queue = queue[1:]
		sorted = append(sorted, curr)

		// Decrease in-degree for aliases this one depends on
		for _, dep := range deps[curr] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	// If we didn't process all aliases, there's a cycle
	if len(sorted) != len(aliases) {
		// Find aliases involved in cycle for error message
		var cycleAliases []string
		for name, degree := range inDegree {
			if degree > 0 {
				cycleAliases = append(cycleAliases, name)
			}
		}
		return nil, fmt.Errorf("circular dependency detected among aliases: %v", cycleAliases)
	}

	// Reverse to get dependencies-first order
	for i, j := 0, len(sorted)-1; i < j; i, j = i+1, j-1 {
		sorted[i], sorted[j] = sorted[j], sorted[i]
	}

	return sorted, nil
}

// buildAliasASTs parses and expands aliases in dependency order.
// Returns a map of alias name to expanded AST.
func buildAliasASTs(aliases map[string]string, order []string) (map[string]ast.Node, error) {
	result := make(map[string]ast.Node, len(order))

	for _, name := range order {
		source := aliases[name]

		// Parse the alias expression
		tree, err := parser.Parse(source)
		if err != nil {
			return nil, fmt.Errorf("alias %q: %w", name, err)
		}

		// If this alias references other aliases, patch them in
		if len(result) > 0 {
			patcher := &aliasPatcher{aliases: result}
			ast.Walk(&tree.Node, patcher)
		}

		result[name] = tree.Node
	}

	return result, nil
}

// divisionValidator walks the AST to find unsafe division operations.
// Reports errors for:
// - Division by literal zero
// - Division with dynamic divisor (should use divOr instead)
type divisionValidator struct {
	errors []string
}

func (v *divisionValidator) Visit(node *ast.Node) {
	if node == nil || *node == nil {
		return
	}

	binNode, ok := (*node).(*ast.BinaryNode)
	if !ok || binNode.Operator != "/" {
		return
	}

	// Check the divisor (right operand)
	switch r := binNode.Right.(type) {
	case *ast.IntegerNode:
		if r.Value == 0 {
			v.errors = append(v.errors, "division by zero")
		}
		// Non-zero integer literal is safe
	case *ast.FloatNode:
		if r.Value == 0 {
			v.errors = append(v.errors, "division by zero")
		}
		// Non-zero float literal is safe
	default:
		// Dynamic divisor - require divOr for safety
		v.errors = append(v.errors, "dynamic divisor in division - use divOr(a, b, fallback) instead of a / b")
	}
}

// ValidateDivisions checks an expression for unsafe division operations.
// Returns an error if:
// - Expression contains division by literal zero
// - Expression contains division with dynamic divisor (should use divOr)
func ValidateDivisions(exprStr string) error {
	tree, err := parser.Parse(exprStr)
	if err != nil {
		return err // Syntax error handled elsewhere
	}

	validator := &divisionValidator{}
	ast.Walk(&tree.Node, validator)

	if len(validator.errors) > 0 {
		return fmt.Errorf("%s", validator.errors[0])
	}
	return nil
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
