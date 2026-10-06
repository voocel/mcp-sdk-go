package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	invopop "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// invopopSchema aliases the inference schema type used across the package.
type invopopSchema = invopop.Schema

// compiledSchema aliases the validation schema type used across the package.
type compiledSchema = jsonschema.Schema

// The single compiled-schema cache. Note that santhosh-tekuri/jsonschema v6
// has no default URL loader, so network $ref values are never dereferenced —
// exactly what the spec requires by default.
var (
	schemaValidatorCache = make(map[string]*jsonschema.Schema)
	validatorCacheMu     sync.RWMutex
)

// inferSchema generates a JSON Schema for T via reflection. Property
// descriptions come from `jsonschema` struct tags. As a special case, `any`
// infers an empty object schema.
func inferSchema[T any]() (*invopop.Schema, error) {
	rt := reflect.TypeFor[T]()
	if rt == reflect.TypeFor[any]() {
		return &invopop.Schema{Type: "object"}, nil
	}
	reflector := &invopop.Reflector{
		AllowAdditionalProperties: true,
		DoNotReference:            true,
	}
	schema := reflector.ReflectFromType(rt)
	if schema == nil {
		return nil, fmt.Errorf("failed to generate schema for type %v", rt)
	}
	return schema, nil
}

// schemaToMap converts an invopop schema to the wire representation.
func schemaToMap(schema *invopop.Schema) (protocol.JSONSchema, error) {
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}
	var schemaMap protocol.JSONSchema
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}
	return schemaMap, nil
}

// compileRawSchema compiles a wire-format schema, sharing the validator cache.
// It never round-trips through the inference type, so keywords unknown to that
// type survive.
func compileRawSchema(m protocol.JSONSchema) (*jsonschema.Schema, error) {
	schemaBytes, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}
	return compileSchemaBytes(schemaBytes)
}

func compileSchemaBytes(schemaBytes []byte) (*jsonschema.Schema, error) {
	schemaKey := string(schemaBytes)

	validatorCacheMu.RLock()
	compiled, exists := schemaValidatorCache[schemaKey]
	validatorCacheMu.RUnlock()
	if exists {
		return compiled, nil
	}

	compiler := jsonschema.NewCompiler()
	var schemaAny any
	if err := json.Unmarshal(schemaBytes, &schemaAny); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}
	if err := compiler.AddResource("schema.json", schemaAny); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}
	compiled, cerr := compiler.Compile("schema.json")
	if cerr != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", cerr)
	}

	validatorCacheMu.Lock()
	schemaValidatorCache[schemaKey] = compiled
	validatorCacheMu.Unlock()
	return compiled, nil
}

// applyDefaults fills missing fields that declare a default value.
func applyDefaults(data map[string]any, schema *invopop.Schema) {
	if schema.Properties == nil {
		return
	}
	for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
		key := pair.Key
		propSchema := pair.Value
		if _, exists := data[key]; !exists && propSchema.Default != nil {
			data[key] = propSchema.Default
		}
		if val, ok := data[key].(map[string]any); ok && propSchema.Type == "object" {
			applyDefaults(val, propSchema)
		}
	}
}

// unmarshalAndValidate fills data's declared defaults, validates it against
// the schema compiled at registration, and unmarshals it into T.
func unmarshalAndValidate[T any](data map[string]any, defaults *invopop.Schema, compiled *compiledSchema) (T, error) {
	var zero T
	applyDefaults(data, defaults)
	if err := compiled.Validate(data); err != nil {
		return zero, fmt.Errorf("validation failed: %w", err)
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

func getZeroValue[T any]() any {
	var zero T
	return zero
}
