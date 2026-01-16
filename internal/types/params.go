// Package types provides shared types and utilities for parameter handling.
package types

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParamConfig defines a parameter for queries or workflow triggers.
type ParamConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // string, int, integer, float, double, bool, boolean, datetime, date, json, int[], string[], float[], bool[]
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
}

// ValidParamTypes defines all valid parameter types
var ValidParamTypes = map[string]bool{
	"string":   true,
	"int":      true,
	"integer":  true,
	"float":    true,
	"double":   true,
	"bool":     true,
	"boolean":  true,
	"datetime": true,
	"date":     true,
	"json":     true,
	"int[]":    true,
	"string[]": true,
	"float[]":  true,
	"bool[]":   true,
}

// IsArrayType returns true if the type is an array type (e.g., "int[]", "string[]")
func IsArrayType(typeName string) bool {
	return len(typeName) > 2 && typeName[len(typeName)-2:] == "[]"
}

// ArrayBaseType returns the base type of an array type (e.g., "int[]" -> "int")
func ArrayBaseType(typeName string) string {
	if IsArrayType(typeName) {
		return typeName[:len(typeName)-2]
	}
	return typeName
}

// ConvertValue converts a string value to the appropriate Go type based on parameter type.
func ConvertValue(value, typeName string) (any, error) {
	lowerType := strings.ToLower(typeName)

	if lowerType == "json" {
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		jsonBytes, err := json.Marshal(parsed)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize JSON: %w", err)
		}
		return string(jsonBytes), nil
	}

	if IsArrayType(lowerType) {
		var arr []any
		if err := json.Unmarshal([]byte(value), &arr); err != nil {
			return nil, fmt.Errorf("expected JSON array: %w", err)
		}
		baseType := ArrayBaseType(lowerType)
		validated, err := ValidateArrayElements(arr, baseType)
		if err != nil {
			return nil, err
		}
		jsonBytes, err := json.Marshal(validated)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize array: %w", err)
		}
		return string(jsonBytes), nil
	}

	switch lowerType {
	case "int", "integer":
		return strconv.Atoi(value)
	case "bool", "boolean":
		return strconv.ParseBool(value)
	case "datetime", "date":
		formats := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, value); err == nil {
				return t, nil
			}
		}
		return nil, fmt.Errorf("invalid datetime format")
	case "float", "double":
		return strconv.ParseFloat(value, 64)
	default:
		return value, nil
	}
}

// ConvertJSONValue converts a JSON-decoded value to the appropriate Go type.
func ConvertJSONValue(v any, typeName string) (any, error) {
	lowerType := strings.ToLower(typeName)

	if lowerType == "json" {
		// Keep native Go types for JSON parameters (arrays, objects)
		// This allows iteration expressions like "trigger.params.tasks" to work with arrays
		return v, nil
	}

	if IsArrayType(lowerType) {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected array, got %T", v)
		}
		baseType := ArrayBaseType(lowerType)
		validated, err := ValidateArrayElements(arr, baseType)
		if err != nil {
			return nil, err
		}
		jsonBytes, err := json.Marshal(validated)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize array: %w", err)
		}
		return string(jsonBytes), nil
	}

	switch lowerType {
	case "int", "integer":
		switch val := v.(type) {
		case float64:
			return int(val), nil
		case string:
			return strconv.Atoi(val)
		default:
			return nil, fmt.Errorf("expected integer, got %T", v)
		}
	case "float", "double":
		switch val := v.(type) {
		case float64:
			return val, nil
		case string:
			return strconv.ParseFloat(val, 64)
		default:
			return nil, fmt.Errorf("expected number, got %T", v)
		}
	case "bool", "boolean":
		switch val := v.(type) {
		case bool:
			return val, nil
		case string:
			return strconv.ParseBool(val)
		default:
			return nil, fmt.Errorf("expected boolean, got %T", v)
		}
	case "datetime", "date":
		strVal, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string for datetime, got %T", v)
		}
		formats := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, strVal); err == nil {
				return t, nil
			}
		}
		return nil, fmt.Errorf("invalid datetime format")
	default:
		switch val := v.(type) {
		case string:
			return val, nil
		case float64:
			return strconv.FormatFloat(val, 'f', -1, 64), nil
		case bool:
			return strconv.FormatBool(val), nil
		case nil:
			return "", nil
		default:
			return fmt.Sprintf("%v", v), nil
		}
	}
}

// ValidateArrayElements validates and converts array elements to the appropriate type.
func ValidateArrayElements(arr []any, baseType string) ([]any, error) {
	result := make([]any, len(arr))
	for i, elem := range arr {
		switch baseType {
		case "int", "integer":
			switch val := elem.(type) {
			case float64:
				result[i] = int(val)
			default:
				return nil, fmt.Errorf("array element %d: expected integer, got %T", i, elem)
			}
		case "float", "double":
			switch val := elem.(type) {
			case float64:
				result[i] = val
			default:
				return nil, fmt.Errorf("array element %d: expected number, got %T", i, elem)
			}
		case "bool", "boolean":
			switch val := elem.(type) {
			case bool:
				result[i] = val
			default:
				return nil, fmt.Errorf("array element %d: expected boolean, got %T", i, elem)
			}
		case "string":
			switch val := elem.(type) {
			case string:
				result[i] = val
			default:
				return nil, fmt.Errorf("array element %d: expected string, got %T", i, elem)
			}
		default:
			result[i] = elem
		}
	}
	return result, nil
}
