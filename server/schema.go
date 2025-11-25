package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	invopop "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	schemaValidatorCache = make(map[string]*jsonschema.Schema)
	validatorCacheMutex  sync.RWMutex
)

// inferSchema Inferring JSON Schema from Type T
func inferSchema[T any](customTypes ...map[reflect.Type]*invopop.Schema) (*invopop.Schema, error) {
	rt := reflect.TypeFor[T]()

	// If the type is any, return a generic object schema.
	if rt == reflect.TypeFor[any]() {
		return &invopop.Schema{
			Type: "object",
		}, nil
	}

	reflector := &invopop.Reflector{
		AllowAdditionalProperties: true, // Allow additional properties, handled by validator
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

	var zero T
	schema := reflector.Reflect(zero)
	if schema == nil {
		return nil, fmt.Errorf("failed to generate schema for type %v", rt)
	}

	if schema.Type != "object" {
		return nil, fmt.Errorf("schema must have type 'object', got %q", schema.Type)
	}

	return schema, nil
}

// compileSchema compiles JSON Schema and caches the result
func compileSchema(schema *invopop.Schema) (*jsonschema.Schema, error) {
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}
	schemaKey := string(schemaBytes)

	// Check cache
	validatorCacheMutex.RLock()
	compiledSchema, exists := schemaValidatorCache[schemaKey]
	validatorCacheMutex.RUnlock()

	if exists {
		return compiledSchema, nil
	}

	// Compile schema
	compiler := jsonschema.NewCompiler()

	var schemaInterface interface{}
	if err := json.Unmarshal(schemaBytes, &schemaInterface); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	if err := compiler.AddResource("schema.json", schemaInterface); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	compiledSchema, err = compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	// Cache compiled schema
	validatorCacheMutex.Lock()
	schemaValidatorCache[schemaKey] = compiledSchema
	validatorCacheMutex.Unlock()

	return compiledSchema, nil
}

// applyDefaults applies default values to data
func applyDefaults(data map[string]any, schema *invopop.Schema) {
	if schema.Properties == nil {
		return
	}

	for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
		key := pair.Key
		propSchema := pair.Value

		// Apply default value if field doesn't exist and has a default
		if _, exists := data[key]; !exists && propSchema.Default != nil {
			data[key] = propSchema.Default
		}

		// Recursively handle nested objects
		if val, ok := data[key].(map[string]any); ok && propSchema.Type == "object" {
			applyDefaults(val, propSchema)
		}
	}
}

// applySchema applies defaults and validates data
func applySchema(data map[string]any, schema *invopop.Schema) error {
	// Apply defaults
	applyDefaults(data, schema)

	// Compile and cache schema
	compiledSchema, err := compileSchema(schema)
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	// Perform full JSON Schema validation
	if err := compiledSchema.Validate(data); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}

// unmarshalAndValidate unmarshals map data and validates it as type T
func unmarshalAndValidate[T any](data map[string]any, schema *invopop.Schema) (T, error) {
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
