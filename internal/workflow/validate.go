package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/robfig/cron/v3"
)

// ValidationResult holds workflow validation results.
type ValidationResult struct {
	Valid    bool
	Errors   []string
	Warnings []string
}

func (r *ValidationResult) addError(format string, args ...any) {
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
	r.Valid = false
}

func (r *ValidationResult) addWarning(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// ValidationContext provides external resources for validation.
type ValidationContext struct {
	Databases      map[string]bool // Database name -> isReadOnly
	RateLimitPools map[string]bool // Rate limit pool names
}

// Validate validates a workflow configuration.
func Validate(cfg *WorkflowConfig, ctx *ValidationContext) *ValidationResult {
	r := &ValidationResult{Valid: true}
	prefix := fmt.Sprintf("workflow[%s]", cfg.Name)

	// Name is required
	if cfg.Name == "" {
		r.addError("workflow: name is required")
		return r
	}

	// Validate triggers
	if len(cfg.Triggers) == 0 {
		r.addError("%s: at least one trigger is required", prefix)
	}
	hasHTTPTrigger := false
	hasCronTrigger := false
	httpRoutes := make(map[string]bool) // "METHOD /path" -> seen

	for i, trig := range cfg.Triggers {
		trigPrefix := fmt.Sprintf("%s.triggers[%d]", prefix, i)
		validateTrigger(&trig, trigPrefix, ctx, r)

		if trig.Type == "http" {
			hasHTTPTrigger = true
			route := trig.Method + " " + trig.Path
			if httpRoutes[route] {
				r.addError("%s: duplicate route '%s'", trigPrefix, route)
			}
			httpRoutes[route] = true
		} else if trig.Type == "cron" {
			hasCronTrigger = true
		}
	}

	// Validate condition aliases
	for name, expr := range cfg.Conditions {
		if err := validateExprSyntax(expr); err != nil {
			r.addError("%s.conditions[%s]: invalid expression: %v", prefix, name, err)
		}
	}

	// Validate steps
	if len(cfg.Steps) == 0 {
		r.addError("%s: at least one step is required", prefix)
	}

	stepNames := make(map[string]int) // name -> index (for forward-reference checking)
	hasResponseStep := false

	for i, step := range cfg.Steps {
		stepName := step.Name
		if stepName == "" {
			stepName = fmt.Sprintf("#%d", i)
		}
		stepPrefix := fmt.Sprintf("%s.steps[%s]", prefix, stepName)

		// Track step names for reference validation
		if step.Name != "" {
			if _, exists := stepNames[step.Name]; exists {
				r.addError("%s: duplicate step name", stepPrefix)
			}
			stepNames[step.Name] = i
		}

		validateStep(&step, stepPrefix, stepNames, ctx, r)

		// Track response steps
		if step.IsResponse() {
			hasResponseStep = true
		}
	}

	// Response step validation
	if hasHTTPTrigger && !hasResponseStep {
		r.addWarning("%s: HTTP trigger but no response step - will return 500 if reached", prefix)
	}
	if hasCronTrigger && !hasHTTPTrigger && hasResponseStep {
		r.addError("%s: response steps are only valid for HTTP triggers", prefix)
	}

	// Check for multiple unconditional response steps
	unconditionalResponses := 0
	conditionalResponses := 0
	for _, step := range cfg.Steps {
		if step.IsResponse() {
			if step.Condition == "" {
				unconditionalResponses++
			} else {
				conditionalResponses++
			}
		}
	}
	if unconditionalResponses > 1 {
		r.addError("%s: multiple unconditional response steps - only one will execute", prefix)
	}
	if hasHTTPTrigger && conditionalResponses > 0 && unconditionalResponses == 0 {
		r.addWarning("%s: all response steps have conditions - requests may return default response if no condition matches", prefix)
	}

	// Auto-naming: require names for multi-step workflows
	if len(cfg.Steps) > 1 {
		for i, step := range cfg.Steps {
			if step.Name == "" && !step.IsResponse() {
				stepPrefix := fmt.Sprintf("%s.steps[#%d]", prefix, i)
				r.addError("%s: name required in multi-step workflow", stepPrefix)
			}
		}
	}

	return r
}

func validateTrigger(cfg *TriggerConfig, prefix string, ctx *ValidationContext, r *ValidationResult) {
	if cfg.Type == "" {
		r.addError("%s: type is required (http or cron)", prefix)
		return
	}
	if !ValidTriggerTypes[cfg.Type] {
		r.addError("%s: invalid type '%s' (must be http or cron)", prefix, cfg.Type)
		return
	}

	switch cfg.Type {
	case "http":
		validateHTTPTrigger(cfg, prefix, ctx, r)
	case "cron":
		validateCronTrigger(cfg, prefix, r)
	}
}

func validateHTTPTrigger(cfg *TriggerConfig, prefix string, ctx *ValidationContext, r *ValidationResult) {
	if cfg.Path == "" {
		r.addError("%s: path is required for http trigger", prefix)
	} else {
		if !strings.HasPrefix(cfg.Path, "/") {
			r.addError("%s: path must start with '/'", prefix)
		}
		if strings.HasPrefix(cfg.Path, "/_/") {
			r.addError("%s: path cannot start with '/_/' (reserved)", prefix)
		}
	}

	if cfg.Method == "" {
		r.addError("%s: method is required for http trigger", prefix)
	} else if !isValidHTTPMethod(cfg.Method) {
		r.addError("%s: method must be GET, POST, PUT, DELETE, PATCH, HEAD, or OPTIONS", prefix)
	}

	// Extract path parameters from path (e.g., /api/items/{id})
	pathParams := extractPathParams(cfg.Path)

	// Validate parameters
	paramNames := make(map[string]bool)
	for i, param := range cfg.Parameters {
		paramPrefix := fmt.Sprintf("%s.parameters[%d]", prefix, i)
		if param.Name == "" {
			r.addError("%s: name is required", paramPrefix)
			continue
		}
		if paramNames[param.Name] {
			r.addError("%s: duplicate parameter name '%s'", paramPrefix, param.Name)
		}
		paramNames[param.Name] = true

		if param.Name == "_timeout" || param.Name == "_nocache" {
			r.addError("%s: '%s' is a reserved parameter name", paramPrefix, param.Name)
		}

		if param.Type != "" && !isValidParamType(param.Type) {
			r.addError("%s: invalid type '%s'", paramPrefix, param.Type)
		}

		// Path parameters must be required (can't have optional path segments)
		if pathParams[param.Name] && !param.Required {
			r.addError("%s: path parameter '%s' must be required", paramPrefix, param.Name)
		}
	}

	// Ensure all path parameters have corresponding parameter definitions
	for pathParam := range pathParams {
		if !paramNames[pathParam] {
			r.addError("%s: path parameter '{%s}' must be defined in parameters", prefix, pathParam)
		}
	}

	// Validate cache
	if cfg.Cache != nil && cfg.Cache.Enabled {
		cachePrefix := prefix + ".cache"
		if cfg.Cache.Key == "" {
			r.addError("%s: key is required when cache is enabled", cachePrefix)
		}
		if cfg.Cache.TTLSec < 0 {
			r.addError("%s: ttl_sec cannot be negative", cachePrefix)
		}
		if cfg.Cache.EvictCron != "" {
			if err := validateCronExpr(cfg.Cache.EvictCron); err != nil {
				r.addError("%s: invalid evict_cron: %v", cachePrefix, err)
			}
		}
	}

	// Validate rate limits
	for i, rl := range cfg.RateLimit {
		rlPrefix := fmt.Sprintf("%s.rate_limit[%d]", prefix, i)
		validateRateLimit(&rl, rlPrefix, ctx, r)
	}
}

func validateCronTrigger(cfg *TriggerConfig, prefix string, r *ValidationResult) {
	if cfg.Schedule == "" {
		r.addError("%s: schedule is required for cron trigger", prefix)
	} else if err := validateCronExpr(cfg.Schedule); err != nil {
		r.addError("%s: invalid schedule: %v", prefix, err)
	}

	// Cron triggers shouldn't have HTTP-specific fields
	if cfg.Path != "" {
		r.addWarning("%s: path is ignored for cron trigger", prefix)
	}
	if cfg.Method != "" {
		r.addWarning("%s: method is ignored for cron trigger", prefix)
	}
	if cfg.Cache != nil {
		r.addWarning("%s: cache is ignored for cron trigger", prefix)
	}
}

func validateRateLimit(cfg *RateLimitRefConfig, prefix string, ctx *ValidationContext, r *ValidationResult) {
	isPool := cfg.Pool != ""
	isInline := cfg.RequestsPerSecond > 0 || cfg.Burst > 0 || cfg.Key != ""

	if isPool && isInline {
		r.addError("%s: cannot specify both pool and inline settings", prefix)
		return
	}
	if !isPool && !isInline {
		r.addError("%s: must specify pool or inline settings", prefix)
		return
	}

	if isPool {
		if ctx != nil && !ctx.RateLimitPools[cfg.Pool] {
			r.addError("%s: unknown rate limit pool '%s'", prefix, cfg.Pool)
		}
	} else {
		if cfg.RequestsPerSecond <= 0 {
			r.addError("%s: requests_per_second must be positive", prefix)
		}
		if cfg.Burst <= 0 {
			r.addError("%s: burst must be positive", prefix)
		}
	}
}

func validateStep(cfg *StepConfig, prefix string, stepNames map[string]int, ctx *ValidationContext, r *ValidationResult) {
	// Validate condition syntax
	if cfg.Condition != "" {
		if err := validateExprSyntax(cfg.Condition); err != nil {
			r.addError("%s.condition: invalid expression: %v", prefix, err)
		}
	}

	// Validate on_error
	if cfg.OnError != "" && !ValidOnErrorValues[cfg.OnError] {
		r.addError("%s: on_error must be 'abort' or 'continue'", prefix)
	}

	// Determine step type
	stepType := cfg.StepType()
	if stepType == "unknown" {
		r.addError("%s: must specify type or provide type-specific fields (sql, url, template, or steps)", prefix)
		return
	}

	// Block validation: steps with nested steps cannot have type or leaf-specific fields
	if cfg.IsBlock() {
		if cfg.Type != "" {
			r.addError("%s: step with nested steps cannot have type (got type: %s)", prefix, cfg.Type)
		}
		if cfg.SQL != "" {
			r.addError("%s: step with nested steps cannot have sql", prefix)
		}
		if cfg.URL != "" {
			r.addError("%s: step with nested steps cannot have url", prefix)
		}
		if cfg.Template != "" {
			r.addError("%s: step with nested steps cannot have template", prefix)
		}
	}

	// Leaf step validation: iterate requires nested steps
	if !cfg.IsBlock() && cfg.Iterate != nil {
		r.addError("%s: iterate requires nested steps", prefix)
	}

	// Type-specific validation
	switch stepType {
	case "query":
		validateQueryStep(cfg, prefix, ctx, r)
	case "httpcall":
		validateHTTPCallStep(cfg, prefix, r)
	case "response":
		validateResponseStep(cfg, prefix, r)
	case "block":
		validateBlockStep(cfg, prefix, stepNames, ctx, r)
	}
}

func validateQueryStep(cfg *StepConfig, prefix string, ctx *ValidationContext, r *ValidationResult) {
	if cfg.Database == "" {
		r.addError("%s: database is required for query step", prefix)
	} else if ctx != nil {
		isReadOnly, exists := ctx.Databases[cfg.Database]
		if !exists {
			r.addError("%s: unknown database '%s'", prefix, cfg.Database)
		} else if isReadOnly && containsWriteKeyword(cfg.SQL) {
			r.addError("%s: SQL contains write operation but database '%s' is read-only", prefix, cfg.Database)
		}
	}

	if cfg.SQL == "" {
		r.addError("%s: sql is required for query step", prefix)
	} else if containsTemplateInterpolation(cfg.SQL) {
		r.addError("%s: SQL contains template interpolation ({{...}}) which is not allowed - use @param style parameters for safe parameterized queries", prefix)
	}

	// Validate session settings
	if cfg.Isolation != "" && !isValidIsolation(cfg.Isolation) {
		r.addError("%s: invalid isolation level '%s'", prefix, cfg.Isolation)
	}
	if cfg.DeadlockPriority != "" && !isValidDeadlockPriority(cfg.DeadlockPriority) {
		r.addError("%s: invalid deadlock_priority '%s'", prefix, cfg.DeadlockPriority)
	}
}

func validateHTTPCallStep(cfg *StepConfig, prefix string, r *ValidationResult) {
	if cfg.URL == "" {
		r.addError("%s: url is required for httpcall step", prefix)
	}

	if cfg.HTTPMethod != "" && !ValidHTTPMethods[cfg.HTTPMethod] {
		r.addError("%s: invalid http_method '%s'", prefix, cfg.HTTPMethod)
	}

	if cfg.Parse != "" && !ValidParseModes[cfg.Parse] {
		r.addError("%s: invalid parse mode '%s' (must be json, text, or form)", prefix, cfg.Parse)
	}

	if cfg.Retry != nil {
		if cfg.Retry.MaxAttempts < 0 {
			r.addError("%s.retry: max_attempts cannot be negative", prefix)
		}
		if cfg.Retry.InitialBackoffSec < 0 {
			r.addError("%s.retry: initial_backoff_sec cannot be negative", prefix)
		}
		if cfg.Retry.MaxBackoffSec < 0 {
			r.addError("%s.retry: max_backoff_sec cannot be negative", prefix)
		}
	}
}

func validateResponseStep(cfg *StepConfig, prefix string, r *ValidationResult) {
	if cfg.Template == "" {
		r.addError("%s: template is required for response step", prefix)
	}

	if cfg.StatusCode != 0 && (cfg.StatusCode < 100 || cfg.StatusCode > 599) {
		r.addError("%s: status_code must be 100-599", prefix)
	}
}

func validateBlockStep(cfg *StepConfig, prefix string, parentStepNames map[string]int, ctx *ValidationContext, r *ValidationResult) {
	// Validate iterate if present
	if cfg.Iterate != nil {
		iterPrefix := prefix + ".iterate"
		if cfg.Iterate.Over == "" {
			r.addError("%s: over is required", iterPrefix)
		} else if err := validateExprSyntax(cfg.Iterate.Over); err != nil {
			r.addError("%s.over: invalid expression: %v", iterPrefix, err)
		}
		if cfg.Iterate.As == "" {
			r.addError("%s: as is required", iterPrefix)
		}
		if cfg.Iterate.OnError != "" && !ValidIterateOnErrorValues[cfg.Iterate.OnError] {
			r.addError("%s: on_error must be 'abort', 'continue', or 'skip'", iterPrefix)
		}
	}

	// Validate nested steps
	if len(cfg.Steps) == 0 {
		r.addError("%s: block must have at least one step", prefix)
	}

	nestedNames := make(map[string]int)
	for i, step := range cfg.Steps {
		stepName := step.Name
		if stepName == "" {
			stepName = fmt.Sprintf("#%d", i)
		}
		stepPrefix := fmt.Sprintf("%s.steps[%s]", prefix, stepName)

		if step.Name != "" {
			if _, exists := nestedNames[step.Name]; exists {
				r.addError("%s: duplicate step name", stepPrefix)
			}
			nestedNames[step.Name] = i
		}

		// Response steps not allowed in blocks
		if step.IsResponse() {
			r.addError("%s: response steps not allowed in blocks", stepPrefix)
			continue
		}

		validateStep(&step, stepPrefix, nestedNames, ctx, r)
	}

	// Validate outputs reference valid step names or special values
	for name, expr := range cfg.Outputs {
		// Allow special output names
		if expr == "success_count" || expr == "failure_count" || expr == "skipped_count" {
			continue
		}
		// Check if it references a step result
		if err := validateExprSyntax(expr); err != nil {
			r.addError("%s.outputs[%s]: invalid expression: %v", prefix, name, err)
		}
	}
}

func validateCronExpr(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	return err
}

func validateExprSyntax(exprStr string) error {
	_, err := compileCondition(exprStr)
	return err
}

// pathParamRegex matches path parameters like {id} or {user_id}
var pathParamRegex = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

func containsWriteKeyword(sql string) bool {
	upper := strings.ToUpper(sql)
	writeKeywords := []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "TRUNCATE ", "ALTER ", "CREATE ", "EXEC "}
	for _, kw := range writeKeywords {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}

// templateInterpolationRegex matches Go template interpolation patterns {{...}}
var templateInterpolationRegex = regexp.MustCompile(`\{\{[^}]*\}\}`)

// containsTemplateInterpolation checks if SQL contains Go template syntax.
// SQL queries must use @param style parameters, not template interpolation.
func containsTemplateInterpolation(sql string) bool {
	return templateInterpolationRegex.MatchString(sql)
}

// Helper validation functions

var validParamTypes = map[string]bool{
	"string": true, "int": true, "integer": true, "float": true, "double": true,
	"bool": true, "boolean": true, "datetime": true, "date": true, "json": true,
	"int[]": true, "string[]": true, "float[]": true, "bool[]": true,
}

func isValidParamType(t string) bool {
	return validParamTypes[strings.ToLower(t)]
}

var validIsolationLevels = map[string]bool{
	"read_uncommitted": true, "read_committed": true, "repeatable_read": true,
	"serializable": true, "snapshot": true,
}

func isValidIsolation(level string) bool {
	return validIsolationLevels[level]
}

var validDeadlockPriorities = map[string]bool{
	"low": true, "normal": true, "high": true,
}

func isValidDeadlockPriority(p string) bool {
	return validDeadlockPriorities[p]
}

func isValidHTTPMethod(method string) bool {
	// Empty string is not valid for triggers (must be explicit)
	if method == "" {
		return false
	}
	return ValidHTTPMethods[method]
}

// extractPathParams extracts path parameter names from a path like /api/items/{id}/details/{subId}
// Returns a map of parameter names (e.g., {"id": true, "subId": true})
func extractPathParams(path string) map[string]bool {
	params := make(map[string]bool)
	matches := pathParamRegex.FindAllStringSubmatch(path, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			params[match[1]] = true
		}
	}
	return params
}

// ExtractPathParams is the exported version for use by handler
func ExtractPathParams(path string) map[string]bool {
	return extractPathParams(path)
}
