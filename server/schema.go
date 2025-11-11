package server

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"
)

// inferSchema Inferring JSON Schema from Type T
func inferSchema[T any](customTypes ...map[reflect.Type]*jsonschema.Schema) (*jsonschema.Schema, error) {
	rt := reflect.TypeFor[T]()

	// If the type is any, return a generic object schema.
	if rt == reflect.TypeFor[any]() {
		return &jsonschema.Schema{
			Type: "object",
		}, nil
	}

	reflector := &jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true, // Inline directly without using $ref
	}

	// Add custom type schemas
	if len(customTypes) > 0 && customTypes[0] != nil {
		for typ := range customTypes[0] {
			reflector.AddGoComments("", typ.PkgPath())
			// Note: invopop/jsonschema doesn't have direct type override
			// But we can customize through Reflector methods
		}
	}

	schema := reflector.Reflect(rt)
	if schema == nil {
		return nil, fmt.Errorf("failed to generate schema for type %v", rt)
	}

	if schema.Type != "object" {
		return nil, fmt.Errorf("schema must have type 'object', got %q", schema.Type)
	}

	return schema, nil
}

func applySchema(data map[string]any, schema *jsonschema.Schema) error {
	// invopop/jsonschema doesn't have built-in validation
	// We can implement basic default value application
	if schema.Properties != nil {
		for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			key := pair.Key
			propSchema := pair.Value
			// Apply default values
			if _, exists := data[key]; !exists && propSchema.Default != nil {
				data[key] = propSchema.Default
			}
		}
	}

	// For full validation, we would need a separate JSON Schema validator
	// For now, we'll do basic validation
	return validateSchema(data, schema)
}

func validateSchema(data map[string]any, schema *jsonschema.Schema) error {
	// Check required fields
	for _, required := range schema.Required {
		if _, exists := data[required]; !exists {
			return fmt.Errorf("missing required field: %s", required)
		}
	}

	// Check property types (basic)
	if schema.Properties != nil {
		for key, value := range data {
			propSchema, ok := schema.Properties.Get(key)
			// AdditionalProperties can be nil (allow any), a Schema, or false
			allowAdditional := schema.AdditionalProperties != nil
			if !ok && !allowAdditional {
				return fmt.Errorf("unexpected field: %s", key)
			}
			if ok {
				if err := validateValue(value, propSchema); err != nil {
					return fmt.Errorf("field %s: %w", key, err)
				}
			}
		}
	}

	return nil
}

func validateValue(value interface{}, schema *jsonschema.Schema) error {
	if value == nil {
		return nil
	}

	switch schema.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "number":
		switch value.(type) {
		case float64, float32, int, int64, int32:
			// OK
		default:
			return fmt.Errorf("expected number, got %T", value)
		}
	case "integer":
		switch value.(type) {
		case int, int64, int32, float64:
			// OK (JSON numbers are float64)
		default:
			return fmt.Errorf("expected integer, got %T", value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case "array":
		if _, ok := value.([]interface{}); !ok {
			return fmt.Errorf("expected array, got %T", value)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
	}

	return nil
}

// unmarshalAndValidate Unmarshal map data and validate it as type T
func unmarshalAndValidate[T any](data map[string]any, schema *jsonschema.Schema) (T, error) {
	var zero T
	if err := applySchema(data, schema); err != nil {
		return zero, err
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return zero, fmt.Errorf("failed to marshal data: %w", err)
	}

	var result T
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		return zero, fmt.Errorf("failed to unmarshal to target type: %w", err)
	}

	return result, nil
}

func getZeroValue[T any]() interface{} {
	var zero T
	return zero
}
