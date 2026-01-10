package utils

import (
	"encoding/json"
	"fmt"
	"reflect"

	invopop "github.com/invopop/jsonschema"
	"github.com/voocel/mcp-sdk-go/protocol"
)

// InferSchemaFromType 使用 invopop/jsonschema 生成对象类型的 JSON Schema。
func InferSchemaFromType(rt reflect.Type, customTypes map[reflect.Type]*invopop.Schema) (*invopop.Schema, error) {
	if rt == nil {
		return nil, fmt.Errorf("nil type")
	}
	if rt == reflect.TypeFor[any]() {
		return &invopop.Schema{Type: "object"}, nil
	}

	reflector := &invopop.Reflector{
		AllowAdditionalProperties: true,
		DoNotReference:            true,
	}
	if len(customTypes) > 0 {
		for typ := range customTypes {
			reflector.AddGoComments("", typ.PkgPath())
		}
	}

	schema := reflector.ReflectFromType(rt)
	if schema == nil {
		return nil, fmt.Errorf("failed to generate schema for type %v", rt)
	}
	if schema.Type != "object" {
		return nil, fmt.Errorf("schema must have type 'object', got %q", schema.Type)
	}
	return schema, nil
}

// SchemaToJSONMap 将 invopop.Schema 转为协议使用的 JSON Schema 格式。
func SchemaToJSONMap(schema *invopop.Schema) (protocol.JSONSchema, error) {
	if schema == nil {
		return nil, fmt.Errorf("nil schema")
	}
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}
	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}
	return schemaMap, nil
}
