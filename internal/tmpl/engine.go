// Package tmpl provides a unified template engine for cache keys, rate limit keys,
// and response templates. All templates use the same context variables and functions.
package tmpl

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"text/template/parse"
	"time"

	"github.com/google/uuid"

	"sql-proxy/internal/publicid"
)

// Usage indicates what context is available for a template
type Usage int

const (
	// UsagePreQuery is for templates evaluated before query execution (cache keys, rate limits)
	UsagePreQuery Usage = iota
)

// Engine manages compiled templates with consistent context and functions
type Engine struct {
	mu        sync.RWMutex
	templates map[string]*compiledTemplate
	funcs     template.FuncMap
	publicIDs *publicid.Encoder // Optional public ID encoder
}

type compiledTemplate struct {
	tmpl  *template.Template
	usage Usage
}

// BaseFuncMap returns a FuncMap with all static template functions.
// This can be used by other packages (like workflow compile) that need
// the same functions at compile time. Note: publicID and privateID
// require an engine instance and are not included here.
func BaseFuncMap() template.FuncMap {
	return template.FuncMap{
		// Strict access - errors if key missing or empty
		"require": requireFunc,

		// Optional access with explicit fallback
		"getOr": getOrFunc,

		// Check if key exists and is non-empty
		"has": hasFunc,

		// JSON serialization
		"json":       jsonFunc,
		"jsonIndent": jsonIndentFunc,

		// Math - support both int and float64
		"add": func(a, b any) float64 { return toNumber(a) + toNumber(b) },
		"sub": func(a, b any) float64 { return toNumber(a) - toNumber(b) },
		"mul": func(a, b any) float64 { return toNumber(a) * toNumber(b) },
		"div": func(a, b any) float64 {
			bv := toNumber(b)
			if bv == 0 {
				return 0 // Silent fallback to 0 - use divOr for explicit default
			}
			return toNumber(a) / bv
		},
		"divOr": func(a, b, defaultVal any) float64 {
			// Division with explicit default value for division by zero
			bv := toNumber(b)
			if bv == 0 {
				return toNumber(defaultVal)
			}
			return toNumber(a) / bv
		},
		"mod": func(a, b any) float64 {
			av, bv := toNumber(a), toNumber(b)
			if bv == 0 {
				return 0 // Silent fallback to 0 - use modOr for explicit default
			}
			return math.Mod(av, bv)
		},
		"modOr": func(a, b, defaultVal any) float64 {
			// Modulo with explicit default value for division by zero
			av, bv := toNumber(a), toNumber(b)
			if bv == 0 {
				return toNumber(defaultVal)
			}
			return math.Mod(av, bv)
		},
		"round": func(v any) float64 { return math.Round(toNumber(v)) },
		"floor": func(v any) float64 { return math.Floor(toNumber(v)) },
		"ceil":  func(v any) float64 { return math.Ceil(toNumber(v)) },
		"trunc": func(v any) float64 { return math.Trunc(toNumber(v)) },
		"abs":   func(v any) float64 { return math.Abs(toNumber(v)) },
		"min": func(a, b any) float64 {
			return math.Min(toNumber(a), toNumber(b))
		},
		"max": func(a, b any) float64 {
			return math.Max(toNumber(a), toNumber(b))
		},

		// Numeric conversion/formatting
		"int64": func(v any) int64 {
			return int64(toNumber(v))
		},
		"float64": func(v any) float64 {
			return toNumber(v)
		},
		"zeropad": func(v any, width int) string {
			return fmt.Sprintf("%0*d", width, int64(toNumber(v)))
		},
		"pad": func(v any, width int, padChar string) string {
			s := fmt.Sprintf("%v", v)
			if len(padChar) == 0 {
				padChar = " "
			}
			for len(s) < width {
				s = padChar[:1] + s
			}
			return s
		},

		// String manipulation
		"upper":     strings.ToUpper,
		"lower":     strings.ToLower,
		"trim":      strings.TrimSpace,
		"replace":   strings.ReplaceAll,
		"contains":  strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,

		// Default value (for direct field access, not map access)
		"default": defaultFunc,

		// Coalesce - return first non-empty value
		"coalesce": coalesceFunc,

		// Header access with canonical form handling
		"header": headerFunc,

		// Cookie access with default value
		"cookie": cookieFunc,

		// Array helpers
		"first":   firstFunc,
		"last":    lastFunc,
		"len":     lenFunc,
		"pluck":   pluckFunc,
		"isEmpty": isEmptyFunc,

		// Type conversions
		"float":  floatFunc,
		"string": stringFunc,
		"bool":   boolFunc,

		// IP network functions (for rate limiting and caching)
		"ipNetwork":   ipNetworkFunc,
		"ipPrefix":    ipPrefixFunc,
		"normalizeIP": normalizeIPFunc,

		// UUID and random ID generation
		"uuid":      uuidFunc,
		"uuid4":     uuidFunc, // Alias
		"uuidShort": uuidShortFunc,
		"shortID":   shortIDFunc,
		"nanoid":    nanoidFunc,

		// Validation helpers
		"isEmail":   isEmailFunc,
		"isUUID":    isUUIDFunc,
		"isURL":     isURLFunc,
		"isIP":      isIPFunc,
		"isIPv4":    isIPv4Func,
		"isIPv6":    isIPv6Func,
		"isNumeric": isNumericFunc,
		"matches":   matchesFunc,

		// Encoding/hashing
		"urlEncode":      urlEncodeFunc,
		"urlDecode":      urlDecodeFunc,
		"urlDecodeOr":    urlDecodeOrFunc,
		"base64Encode":   base64EncodeFunc,
		"base64Decode":   base64DecodeFunc,
		"base64DecodeOr": base64DecodeOrFunc,
		"sha256":         sha256Func,
		"md5":            md5Func,
		"hmacSHA256":     hmacSHA256Func,

		// String helpers
		"truncate": truncateFunc,
		"split":    splitFunc,
		"join":     joinFunc,
		"quote":    strconv.Quote,
		"sprintf":  fmt.Sprintf,
		"repeat":   strings.Repeat,
		"substr":   substrFunc,

		// Date/time functions
		"now":         nowFunc,
		"formatTime":  formatTimeFunc,
		"parseTime":   parseTimeFunc,
		"parseTimeOr": parseTimeOrFunc,
		"unixTime":    unixTimeFunc,

		// JSON helpers
		"pick":  pickFunc,
		"omit":  omitFunc,
		"merge": mergeFunc,

		// Conditional helpers
		"ternary": ternaryFunc,
		"when":    whenFunc,

		// Safe navigation
		"dig": digFunc,

		// Debug helpers
		"typeOf": typeOfFunc,
		"keys":   keysFunc,
		"values": valuesFunc,

		// Numeric formatting
		"formatNumber":  formatNumberFunc,
		"formatPercent": formatPercentFunc,
		"formatBytes":   formatBytesFunc,

		// Comparison operators for templates
		"eq": reflect.DeepEqual,
		"ne": func(a, b any) bool { return !reflect.DeepEqual(a, b) },
		"lt": func(a, b any) bool { return toNumber(a) < toNumber(b) },
		"le": func(a, b any) bool { return toNumber(a) <= toNumber(b) },
		"gt": func(a, b any) bool { return toNumber(a) > toNumber(b) },
		"ge": func(a, b any) bool { return toNumber(a) >= toNumber(b) },

		// Boolean operators (variadic to support multiple arguments)
		"and": andFunc,
		"or":  orFunc,
		"not": func(a bool) bool { return !a },

		// Printf alias
		"printf": fmt.Sprintf,
	}
}

// New creates a new template engine with all standard functions
func New() *Engine {
	e := &Engine{
		templates: make(map[string]*compiledTemplate),
	}
	// Start with base functions
	e.funcs = BaseFuncMap()
	// Add engine-specific functions (these need the engine instance)
	e.funcs["publicID"] = e.publicIDFunc
	e.funcs["privateID"] = e.privateIDFunc
	return e
}

// SetPublicIDEncoder configures the public ID encoder for publicID/privateID functions
func (e *Engine) SetPublicIDEncoder(enc *publicid.Encoder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.publicIDs = enc
}

// publicIDFunc encodes an internal ID to a public ID
func (e *Engine) publicIDFunc(namespace string, id any) (string, error) {
	e.mu.RLock()
	enc := e.publicIDs
	e.mu.RUnlock()

	if enc == nil {
		return "", fmt.Errorf("publicID: encoder not configured")
	}

	var idVal int64
	switch v := id.(type) {
	case int:
		idVal = int64(v)
	case int64:
		idVal = v
	case float64:
		idVal = int64(v)
	default:
		return "", fmt.Errorf("publicID: invalid id type %T", id)
	}

	return enc.Encode(namespace, idVal)
}

// privateIDFunc decodes a public ID back to an internal ID
func (e *Engine) privateIDFunc(namespace string, publicID string) (int64, error) {
	e.mu.RLock()
	enc := e.publicIDs
	e.mu.RUnlock()

	if enc == nil {
		return 0, fmt.Errorf("privateID: encoder not configured")
	}

	return enc.Decode(namespace, publicID)
}

// requireFunc returns value from map, errors if missing or empty
func requireFunc(m map[string]string, key string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("required key %q: nil map", key)
	}
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("required key %q not found", key)
	}
	if v == "" {
		return "", fmt.Errorf("required key %q is empty", key)
	}
	return v, nil
}

// getOrFunc returns value from map, or fallback if missing/empty
func getOrFunc(m map[string]string, key, fallback string) string {
	if m == nil {
		return fallback
	}
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return fallback
}

// hasFunc checks if key exists and is non-empty
func hasFunc(m map[string]string, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	return ok && v != ""
}

// jsonFunc serializes value to JSON
func jsonFunc(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("[json error: %v]", err)
	}
	return string(b)
}

// jsonIndentFunc serializes value to indented JSON
func jsonIndentFunc(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("[json error: %v]", err)
	}
	return string(b)
}

// defaultFunc returns def if val is empty/zero, otherwise val
func defaultFunc(def, val any) any {
	if val == nil {
		return def
	}
	switch v := val.(type) {
	case string:
		if v == "" {
			return def
		}
	case int:
		if v == 0 {
			return def
		}
	case int64:
		if v == 0 {
			return def
		}
	case float64:
		if v == 0 {
			return def
		}
	case bool:
		// false is a valid value, not "empty"
	}
	return val
}

// coalesceFunc returns first non-empty string
func coalesceFunc(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// andFunc returns true if all arguments are true (variadic)
// Short-circuits on first false value
func andFunc(args ...bool) bool {
	for _, a := range args {
		if !a {
			return false
		}
	}
	return true
}

// orFunc returns true if any argument is true (variadic)
// Short-circuits on first true value
func orFunc(args ...bool) bool {
	for _, a := range args {
		if a {
			return true
		}
	}
	return false
}

// toNumber converts any numeric type to float64
// Also handles string conversion for numeric strings
func toNumber(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint:
		return float64(n)
	case uint8:
		return float64(n)
	case uint16:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	case float32:
		return float64(n)
	case float64:
		return n
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}

// headerFunc returns header value with canonical form handling
func headerFunc(headers any, name string, defaultVal ...string) string {
	def := ""
	if len(defaultVal) > 0 {
		def = defaultVal[0]
	}

	canonical := http.CanonicalHeaderKey(name)

	switch h := headers.(type) {
	case map[string]string:
		if v, ok := h[canonical]; ok && v != "" {
			return v
		}
	case map[string]any:
		if v, ok := h[canonical]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	case http.Header:
		if v := h.Get(name); v != "" {
			return v
		}
	}
	return def
}

// cookieFunc returns cookie value with optional default
func cookieFunc(cookies any, name string, defaultVal ...string) string {
	def := ""
	if len(defaultVal) > 0 {
		def = defaultVal[0]
	}

	switch c := cookies.(type) {
	case map[string]string:
		if v, ok := c[name]; ok && v != "" {
			return v
		}
	case map[string]any:
		if v, ok := c[name]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return def
}

// firstFunc returns first element of slice/array
func firstFunc(arr any) any {
	if arr == nil {
		return nil
	}
	v := reflect.ValueOf(arr)
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		if v.Len() > 0 {
			return v.Index(0).Interface()
		}
	}
	return nil
}

// lastFunc returns last element of slice/array
func lastFunc(arr any) any {
	if arr == nil {
		return nil
	}
	v := reflect.ValueOf(arr)
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		if v.Len() > 0 {
			return v.Index(v.Len() - 1).Interface()
		}
	}
	return nil
}

// lenFunc returns length of slice/array/map/string
func lenFunc(v any) int {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String, reflect.Chan:
		return rv.Len()
	}
	return 0
}

// pluckFunc extracts a field from each element of a slice of maps
func pluckFunc(arr any, field string) []any {
	v := reflect.ValueOf(arr)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil
	}

	result := make([]any, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i).Interface()
		if m, ok := elem.(map[string]any); ok {
			if val, exists := m[field]; exists {
				result = append(result, val)
			}
		}
	}
	return result
}

// isEmptyFunc checks if value is empty (nil, empty string, empty slice/map, zero)
func isEmptyFunc(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Chan:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Ptr, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// floatFunc converts value to float64
func floatFunc(v any) float64 {
	return toNumber(v)
}

// stringFunc converts value to string
func stringFunc(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// boolFunc converts value to bool
func boolFunc(v any) bool {
	if v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b != "" && b != "0" && b != "false" && b != "False" && b != "FALSE"
	case int, int8, int16, int32, int64:
		return reflect.ValueOf(v).Int() != 0
	case uint, uint8, uint16, uint32, uint64:
		return reflect.ValueOf(v).Uint() != 0
	case float32, float64:
		return reflect.ValueOf(v).Float() != 0
	}
	return true // Non-nil, non-zero is truthy
}

// Default prefix lengths for IP network functions
const (
	defaultIPv4Prefix = 32 // Exact IP for IPv4
	defaultIPv6Prefix = 64 // Typical residential allocation for IPv6
)

// ipNetworkFunc returns the network portion of an IP address.
// Usage: ipNetwork "192.168.1.100" -> "192.168.1.100" (default /32 for IPv4)
// Usage: ipNetwork "192.168.1.100" 24 -> "192.168.1.0" (/24 for IPv4)
// Usage: ipNetwork "192.168.1.100" 24 64 -> "192.168.1.0" (/24 for IPv4, /64 for IPv6)
// Usage: ipNetwork "2001:db8::1234" -> "2001:db8::" (default /64 for IPv6)
// IPv4-mapped IPv6 addresses (::ffff:1.2.3.4) are normalized to IPv4 first.
func ipNetworkFunc(ip string, prefixes ...int) string {
	network, _ := getIPNetwork(ip, prefixes...)
	return network
}

// ipPrefixFunc returns the network portion with prefix length (CIDR notation).
// Usage: ipPrefix "192.168.1.100" 24 -> "192.168.1.0/24"
// Usage: ipPrefix "2001:db8::1234" -> "2001:db8::/64"
func ipPrefixFunc(ip string, prefixes ...int) string {
	network, prefix := getIPNetwork(ip, prefixes...)
	if prefix == 0 {
		// Invalid IP (getIPNetwork returns prefix=0 for invalid IPs)
		return ip
	}
	return fmt.Sprintf("%s/%d", network, prefix)
}

// normalizeIPFunc normalizes an IP address.
// - IPv4-mapped IPv6 to plain IPv4: ::ffff:1.2.3.4 -> 1.2.3.4
// - Compresses IPv6: 2001:0db8:0000::1 -> 2001:db8::1
// - Returns original string if invalid
func normalizeIPFunc(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}

	// Check for IPv4-mapped IPv6 address
	if addr.Is4In6() {
		return addr.Unmap().String()
	}

	return addr.String()
}

// getIPNetwork extracts the network portion of an IP address with the given prefix.
// Returns the network string and the prefix length used.
func getIPNetwork(ip string, prefixes ...int) (string, int) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip, 0
	}

	// Handle IPv4-mapped IPv6 addresses
	if addr.Is4In6() {
		addr = addr.Unmap()
	}

	// Determine prefix length
	var prefix int
	if addr.Is4() {
		prefix = defaultIPv4Prefix
		if len(prefixes) > 0 && prefixes[0] > 0 && prefixes[0] <= 32 {
			prefix = prefixes[0]
		}
	} else {
		prefix = defaultIPv6Prefix
		if len(prefixes) > 1 && prefixes[1] > 0 && prefixes[1] <= 128 {
			prefix = prefixes[1]
		} else if len(prefixes) == 1 && prefixes[0] > 0 && prefixes[0] <= 128 {
			// If only one prefix provided and it's valid for IPv6, use it
			prefix = prefixes[0]
		}
	}

	// Create prefix and get network address
	pfx, err := addr.Prefix(prefix)
	if err != nil {
		return ip, 0
	}

	return pfx.Addr().String(), prefix
}

// UUID and random ID functions

// uuidFunc generates a new UUID v4.
// Returns: "550e8400-e29b-41d4-a716-446655440000"
func uuidFunc() string {
	return uuid.New().String()
}

// uuidShortFunc generates a UUID v4 without hyphens.
// Returns: "550e8400e29b41d4a716446655440000"
func uuidShortFunc() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")
}

// base62Alphabet is the character set used for shortID generation
// Uses the constant from publicid package
var base62Alphabet = publicid.Base62Alphabet

// shortIDFunc generates a random base62 ID of the specified length.
// Default length is 12 if not specified. Valid range: 1-32.
// Returns: "AbC3d5fGh12x"
func shortIDFunc(length ...int) string {
	n := 12
	if len(length) > 0 && length[0] > 0 {
		n = length[0]
		if n > 32 {
			n = 32 // Cap at 32 to ensure UUID fallback works
		}
	}

	result := make([]byte, n)
	alphabetLen := big.NewInt(int64(len(base62Alphabet)))
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			// Fallback to UUID-based generation if crypto/rand fails
			return uuidShortFunc()[:n]
		}
		result[i] = base62Alphabet[idx.Int64()]
	}
	return string(result)
}

// NanoID alphabet (URL-safe characters)
const nanoidAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz-"

// nanoidFunc generates a NanoID-style random string of the specified length.
// Default length is 21 if not specified (matching NanoID default). Valid range: 1-32.
// Returns: "V1StGXR8_Z5jdHi6B-myT"
func nanoidFunc(length ...int) string {
	n := 21
	if len(length) > 0 && length[0] > 0 {
		n = length[0]
		if n > 32 {
			n = 32 // Cap at 32 to ensure UUID fallback works
		}
	}

	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to UUID-based generation if crypto/rand fails
		return uuidShortFunc()[:n]
	}

	for i := range bytes {
		bytes[i] = nanoidAlphabet[bytes[i]%64]
	}
	return string(bytes)
}

// Register compiles and stores a named template
func (e *Engine) Register(name, tmplStr string, usage Usage) error {
	if tmplStr == "" {
		return fmt.Errorf("template %q: empty template", name)
	}

	t, err := template.New(name).
		Funcs(e.funcs).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template %q: %w", name, err)
	}

	e.mu.Lock()
	e.templates[name] = &compiledTemplate{
		tmpl:  t,
		usage: usage,
	}
	e.mu.Unlock()
	return nil
}

// Execute runs a registered template with the given context
func (e *Engine) Execute(name string, ctx *Context) (string, error) {
	e.mu.RLock()
	ct, ok := e.templates[name]
	e.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("template %q not registered", name)
	}

	data := ctx.toMap(ct.usage)

	var buf bytes.Buffer
	if err := ct.tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	result := buf.String()
	if result == "" {
		return "", fmt.Errorf("template %q produced empty result", name)
	}

	return result, nil
}

// ExecuteInline compiles and executes a template string directly (not cached)
func (e *Engine) ExecuteInline(tmplStr string, ctx *Context, usage Usage) (string, error) {
	if tmplStr == "" {
		return "", fmt.Errorf("empty template")
	}

	t, err := template.New("inline").
		Funcs(e.funcs).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return "", err
	}

	data := ctx.toMap(usage)

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	result := buf.String()
	if result == "" {
		return "", fmt.Errorf("template produced empty result")
	}

	return result, nil
}

// validPathPrefixes are the valid root paths in template context
var validPathPrefixes = []string{
	".trigger",  // Request data: params, headers, cookies, path, method, etc.
	".steps",    // Step results
	".workflow", // Workflow metadata: name, request_id, start_time
	".iter",     // Block iteration variables
	".parent",   // Parent context in blocks
}

// Validate checks a template string without executing it
func (e *Engine) Validate(tmplStr string, usage Usage) error {
	if tmplStr == "" {
		return fmt.Errorf("empty template")
	}

	// Parse
	t, err := template.New("validate").
		Funcs(e.funcs).
		Option("missingkey=error").
		Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Validate that all template paths use valid prefixes
	if t.Tree != nil && t.Tree.Root != nil {
		paths := make(map[string]bool)
		extractFieldPaths(t.Tree.Root, paths)
		for path := range paths {
			if !isValidPath(path) {
				return fmt.Errorf("invalid template path %q - must start with .trigger, .steps, .workflow, .iter, or .parent", path)
			}
		}
	}

	// Execute with sample data to catch structural errors
	sample := sampleContextMap(usage)
	var buf bytes.Buffer
	if err := t.Execute(&buf, sample); err != nil {
		// Check if error is about missing map key - that's expected for templates
		// that reference headers/query params we don't have in sample
		errStr := err.Error()
		if strings.Contains(errStr, "map has no entry") ||
			strings.Contains(errStr, "not found") ||
			strings.Contains(errStr, "is empty") {
			// These errors are OK - runtime will have the actual data
			return nil
		}
		return fmt.Errorf("execution error: %w", err)
	}

	return nil
}

// ValidateWithParams validates a template and checks that Param references exist
func (e *Engine) ValidateWithParams(tmplStr string, usage Usage, paramNames []string) error {
	if err := e.Validate(tmplStr, usage); err != nil {
		return err
	}

	// Check that trigger.params references exist in paramNames
	paramRefs := ExtractParamRefs(tmplStr)
	paramSet := make(map[string]bool)
	for _, p := range paramNames {
		paramSet[p] = true
	}

	for _, ref := range paramRefs {
		if !paramSet[ref] {
			return fmt.Errorf("template references .trigger.params.%s but no such parameter defined", ref)
		}
	}

	return nil
}

// sampleContextMap returns sample data for validation
func sampleContextMap(usage Usage) map[string]any {
	m := map[string]any{
		"trigger": map[string]any{
			"client_ip": "192.168.1.1",
			"method":    "GET",
			"path":      "/api/sample",
			"params":    map[string]any{},
			"headers":   map[string]string{},
			"query":     map[string]string{},
			"cookies":   map[string]string{},
		},
		"RequestID": "sample-request-id",
		"Timestamp": "2024-01-01T00:00:00Z",
		"Version":   "1.0.0",
	}

	return m
}

// ============================================================================
// Validation helpers
// ============================================================================

// Email regex - simplified but practical
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// regexCache caches compiled regexes to avoid recompilation on repeated patterns.
// Uses sync.Map for concurrent access. Size is bounded to prevent unbounded growth.
var regexCache sync.Map

// regexCacheMaxSize is the maximum number of patterns to cache.
// Once reached, new patterns are compiled but not cached.
const regexCacheMaxSize = 100

// regexCacheSize tracks the approximate size of the cache.
// Not perfectly accurate under concurrent access but sufficient for bounding.
var regexCacheSize int64

// isEmailFunc validates email format
func isEmailFunc(s string) bool {
	return emailRegex.MatchString(s)
}

// isUUIDFunc validates UUID format (v1-v5)
func isUUIDFunc(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// isURLFunc validates URL format
func isURLFunc(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// isIPFunc validates IP address (v4 or v6)
func isIPFunc(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}

// isIPv4Func validates IPv4 address. Returns true for pure IPv4 and IPv4-mapped
// IPv6 addresses (::ffff:x.x.x.x). Mutually exclusive with isIPv6Func.
func isIPv4Func(s string) bool {
	addr, err := netip.ParseAddr(s)
	return err == nil && (addr.Is4() || addr.Is4In6())
}

// isIPv6Func validates IPv6 address. Returns true for pure IPv6, excluding
// IPv4-mapped addresses (::ffff:x.x.x.x). Mutually exclusive with isIPv4Func.
func isIPv6Func(s string) bool {
	addr, err := netip.ParseAddr(s)
	return err == nil && addr.Is6() && !addr.Is4In6()
}

// isNumericFunc checks if string is numeric
func isNumericFunc(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// matchesFunc checks if string matches regex pattern.
// Returns false for invalid patterns (graceful degradation, pattern may be user-provided).
// Security: Go's regexp uses RE2 engine with guaranteed O(n) matching (ReDoS-safe).
func matchesFunc(pattern, s string) bool {
	// Try to get cached regex
	if cached, ok := regexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp).MatchString(s)
	}

	// Compile regex (returns false for invalid patterns)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}

	// Cache if under size limit
	if atomic.LoadInt64(&regexCacheSize) < regexCacheMaxSize {
		if _, loaded := regexCache.LoadOrStore(pattern, re); !loaded {
			atomic.AddInt64(&regexCacheSize, 1)
		}
	}

	return re.MatchString(s)
}

// ============================================================================
// Encoding/hashing
// ============================================================================

// urlEncodeFunc URL-encodes a string
func urlEncodeFunc(s string) string {
	return url.QueryEscape(s)
}

// urlDecodeFunc URL-decodes a string
// Returns original string on error for safety
func urlDecodeFunc(s string) string {
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return s // Return original on error
	}
	return decoded
}

// urlDecodeOrFunc URL-decodes a string with explicit default on error
func urlDecodeOrFunc(s, defaultVal string) string {
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return defaultVal
	}
	return decoded
}

// base64EncodeFunc encodes string to base64
func base64EncodeFunc(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// base64DecodeFunc decodes base64 string
// Returns original string on error for consistency with urlDecode
func base64DecodeFunc(s string) string {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s // Return original on error (consistent with urlDecode)
	}
	return string(decoded)
}

// base64DecodeOrFunc decodes base64 string with explicit default on error
func base64DecodeOrFunc(s, defaultVal string) string {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return defaultVal
	}
	return string(decoded)
}

// sha256Func returns SHA256 hash of string as hex
func sha256Func(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// md5Func returns MD5 hash of string as hex
func md5Func(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// hmacSHA256Func returns HMAC-SHA256 of message with key as hex
func hmacSHA256Func(key, message string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// ============================================================================
// String helpers
// ============================================================================

// truncateFunc truncates string to max length (in runes), adding suffix if truncated
func truncateFunc(s string, maxLen int, suffix ...string) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	sfx := "..."
	if len(suffix) > 0 {
		sfx = suffix[0]
	}
	sfxRunes := []rune(sfx)
	if maxLen <= len(sfxRunes) {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-len(sfxRunes)]) + sfx
}

// splitFunc splits string by separator
func splitFunc(sep, s string) []string {
	return strings.Split(s, sep)
}

// joinFunc joins slice elements with separator
func joinFunc(sep string, arr any) string {
	v := reflect.ValueOf(arr)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return fmt.Sprintf("%v", arr)
	}
	parts := make([]string, v.Len())
	for i := 0; i < v.Len(); i++ {
		parts[i] = fmt.Sprintf("%v", v.Index(i).Interface())
	}
	return strings.Join(parts, sep)
}

// substrFunc extracts substring from start index (in runes) with optional length
func substrFunc(s string, start int, length ...int) string {
	runes := []rune(s)
	runeLen := len(runes)
	if start < 0 {
		start = runeLen + start
	}
	if start < 0 {
		start = 0
	}
	if start >= runeLen {
		return ""
	}
	end := runeLen
	if len(length) > 0 && length[0] >= 0 {
		end = start + length[0]
		if end > runeLen {
			end = runeLen
		}
	}
	return string(runes[start:end])
}

// ============================================================================
// Date/time functions
// ============================================================================

// nowFunc returns current time in specified format
func nowFunc(format ...string) string {
	f := time.RFC3339
	if len(format) > 0 {
		f = convertTimeFormat(format[0])
	}
	return time.Now().UTC().Format(f)
}

// formatTimeFunc formats a time value
func formatTimeFunc(t any, format string) string {
	var tm time.Time
	switch v := t.(type) {
	case time.Time:
		tm = v
	case string:
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return v
		}
		tm = parsed
	case int:
		tm = time.Unix(int64(v), 0)
	case int64:
		tm = time.Unix(v, 0)
	case float64:
		tm = time.Unix(int64(v), 0)
	default:
		return fmt.Sprintf("%v", t)
	}
	return tm.UTC().Format(convertTimeFormat(format))
}

// parseTimeFunc parses time string to Unix timestamp
// Returns 0 on parse error - use parseTimeOr for explicit default
func parseTimeFunc(s string, format ...string) int64 {
	f := time.RFC3339
	if len(format) > 0 {
		f = convertTimeFormat(format[0])
	}
	t, err := time.Parse(f, s)
	if err != nil {
		return 0 // Use parseTimeOr for explicit default
	}
	return t.Unix()
}

// parseTimeOrFunc parses time string to Unix timestamp with explicit default
func parseTimeOrFunc(s string, defaultVal int64, format ...string) int64 {
	f := time.RFC3339
	if len(format) > 0 {
		f = convertTimeFormat(format[0])
	}
	t, err := time.Parse(f, s)
	if err != nil {
		return defaultVal
	}
	return t.Unix()
}

// unixTimeFunc returns current Unix timestamp
func unixTimeFunc() int64 {
	return time.Now().Unix()
}

// convertTimeFormat converts common format strings to Go format
// Replacements are ordered longest-first to prevent partial matches
func convertTimeFormat(format string) string {
	// Order matters: longer patterns must be replaced first
	// Otherwise "YYYY" might have "YY" replaced to "06" first, producing "0606"
	replacements := []struct{ old, new string }{
		{"YYYY", "2006"},
		{"YY", "06"},
		{"SSS", "000"},
		{"MM", "01"},
		{"DD", "02"},
		{"HH", "15"},
		{"mm", "04"},
		{"ss", "05"},
	}
	result := format
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.old, r.new)
	}
	return result
}

// ============================================================================
// JSON helpers
// ============================================================================

// pickFunc returns a map with only the specified keys
// Returns empty map if input is nil
func pickFunc(m map[string]any, keys ...string) map[string]any {
	result := make(map[string]any)
	if m == nil {
		return result
	}
	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	for k, v := range m {
		if keySet[k] {
			result[k] = v
		}
	}
	return result
}

// omitFunc returns a map without the specified keys
// Returns empty map if input is nil
func omitFunc(m map[string]any, keys ...string) map[string]any {
	result := make(map[string]any)
	if m == nil {
		return result
	}
	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	for k, v := range m {
		if !keySet[k] {
			result[k] = v
		}
	}
	return result
}

// mergeFunc merges multiple maps, later values override earlier
func mergeFunc(maps ...map[string]any) map[string]any {
	result := make(map[string]any)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// ============================================================================
// Conditional helpers
// ============================================================================

// ternaryFunc returns trueVal if condition is true, otherwise falseVal
func ternaryFunc(condition bool, trueVal, falseVal any) any {
	if condition {
		return trueVal
	}
	return falseVal
}

// whenFunc returns value if condition is true, otherwise empty string
func whenFunc(condition bool, value any) any {
	if condition {
		return value
	}
	return ""
}

// ============================================================================
// Safe navigation
// ============================================================================

// digFunc safely navigates nested maps/slices, returns nil if path doesn't exist
func digFunc(data any, path ...any) any {
	current := data
	for _, key := range path {
		if current == nil {
			return nil
		}
		switch c := current.(type) {
		case map[string]any:
			if k, ok := key.(string); ok {
				current = c[k]
			} else {
				return nil
			}
		case map[string]string:
			if k, ok := key.(string); ok {
				if v, exists := c[k]; exists {
					current = v
				} else {
					return nil
				}
			} else {
				return nil
			}
		case []any:
			if i, ok := toInt(key); ok && i >= 0 && i < len(c) {
				current = c[i]
			} else {
				return nil
			}
		case []map[string]any:
			if i, ok := toInt(key); ok && i >= 0 && i < len(c) {
				current = c[i]
			} else {
				return nil
			}
		default:
			// Try reflection for other slice types
			v := reflect.ValueOf(current)
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
				if i, ok := toInt(key); ok && i >= 0 && i < v.Len() {
					current = v.Index(i).Interface()
				} else {
					return nil
				}
			} else {
				return nil
			}
		}
	}
	return current
}

// toInt converts various types to int
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i, true
		}
	}
	return 0, false
}

// ============================================================================
// Debug helpers
// ============================================================================

// typeOfFunc returns the type of a value as string
func typeOfFunc(v any) string {
	if v == nil {
		return "nil"
	}
	return reflect.TypeOf(v).String()
}

// keysFunc returns keys of a map in sorted order for deterministic results
func keysFunc(m any) []string {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Map {
		return nil
	}
	keys := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		keys = append(keys, fmt.Sprintf("%v", k.Interface()))
	}
	sort.Strings(keys)
	return keys
}

// valuesFunc returns values of a map in sorted key order for deterministic results
func valuesFunc(m any) []any {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Map {
		return nil
	}
	// Get and sort keys for deterministic iteration order
	mapKeys := v.MapKeys()
	sortedKeys := make([]string, len(mapKeys))
	keyToReflect := make(map[string]reflect.Value, len(mapKeys))
	for i, k := range mapKeys {
		keyStr := fmt.Sprintf("%v", k.Interface())
		sortedKeys[i] = keyStr
		keyToReflect[keyStr] = k
	}
	sort.Strings(sortedKeys)

	values := make([]any, 0, v.Len())
	for _, keyStr := range sortedKeys {
		values = append(values, v.MapIndex(keyToReflect[keyStr]).Interface())
	}
	return values
}

// ============================================================================
// Numeric formatting
// ============================================================================

// formatNumberFunc formats a number with thousand separators
func formatNumberFunc(v any, decimals ...int) string {
	n := toNumber(v)
	dec := 0
	if len(decimals) > 0 {
		dec = decimals[0]
	}

	// Format with decimals
	format := fmt.Sprintf("%%.%df", dec)
	s := fmt.Sprintf(format, n)

	// Add thousand separators
	parts := strings.Split(s, ".")
	intPart := parts[0]

	// Handle negative numbers
	negative := false
	if strings.HasPrefix(intPart, "-") {
		negative = true
		intPart = intPart[1:]
	}

	// Add commas
	var result strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}

	formatted := result.String()
	if negative {
		formatted = "-" + formatted
	}
	if len(parts) > 1 {
		formatted += "." + parts[1]
	}
	return formatted
}

// formatPercentFunc formats a decimal as percentage
func formatPercentFunc(v any, decimals ...int) string {
	n := toNumber(v) * 100
	dec := 1
	if len(decimals) > 0 {
		dec = decimals[0]
	}
	format := fmt.Sprintf("%%.%df%%%%", dec)
	return fmt.Sprintf(format, n)
}

// formatBytesFunc formats bytes as human-readable size.
// Handles negative values correctly (e.g., for difference calculations).
func formatBytesFunc(v any) string {
	b := toNumber(v)
	negative := b < 0
	if negative {
		b = -b
	}
	const unit = 1024
	var result string
	if b < unit {
		result = fmt.Sprintf("%.0f B", b)
	} else {
		div, exp := float64(unit), 0
		for n := b / unit; n >= unit; n /= unit {
			div *= unit
			exp++
		}
		result = fmt.Sprintf("%.1f %cB", b/div, "KMGTPE"[exp])
	}
	if negative {
		return "-" + result
	}
	return result
}

// Template path validation
// ============================================================================

// isValidPath checks if a template path starts with a known valid prefix
func isValidPath(path string) bool {
	for _, prefix := range validPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// extractFieldPaths recursively extracts all field paths from a template parse tree
func extractFieldPaths(node parse.Node, paths map[string]bool) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				extractFieldPaths(child, paths)
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
		extractFieldPaths(n.List, paths)
		extractFieldPaths(n.ElseList, paths)
	case *parse.RangeNode:
		if n.Pipe != nil {
			extractPathsFromPipe(n.Pipe, paths)
		}
		extractFieldPaths(n.List, paths)
		extractFieldPaths(n.ElseList, paths)
	case *parse.WithNode:
		if n.Pipe != nil {
			extractPathsFromPipe(n.Pipe, paths)
		}
		extractFieldPaths(n.List, paths)
		extractFieldPaths(n.ElseList, paths)
	case *parse.TemplateNode:
		if n.Pipe != nil {
			extractPathsFromPipe(n.Pipe, paths)
		}
	}
}

// extractPathsFromPipe extracts field paths from a pipe node
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

// extractPathsFromArg extracts field paths from a command argument
func extractPathsFromArg(arg parse.Node, paths map[string]bool) {
	switch n := arg.(type) {
	case *parse.FieldNode:
		if len(n.Ident) > 0 {
			path := "." + strings.Join(n.Ident, ".")
			paths[path] = true
		}
	case *parse.ChainNode:
		if n.Node != nil {
			extractPathsFromArg(n.Node, paths)
		}
	case *parse.PipeNode:
		extractPathsFromPipe(n, paths)
	}
}
