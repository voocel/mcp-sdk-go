package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
)

func TimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

func StructToJSONSchema(v interface{}) (protocol.JSONSchema, error) {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input must be a struct or pointer to struct")
	}

	schema := protocol.JSONSchema{
		"type":       "object",
		"properties": make(map[string]interface{}),
		"required":   []string{},
	}

	properties := schema["properties"].(map[string]interface{})
	required := schema["required"].([]string)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		jsonName := field.Name
		tagParts := strings.Split(jsonTag, ",")
		if tagParts[0] != "" {
			jsonName = tagParts[0]
		}

		isRequired := !strings.Contains(jsonTag, "omitempty")
		if isRequired {
			required = append(required, jsonName)
		}

		fieldSchema := createFieldSchema(field.Type)
		properties[jsonName] = fieldSchema
	}
	schema["required"] = required
	return schema, nil
}

func createFieldSchema(t reflect.Type) map[string]interface{} {
	schema := make(map[string]interface{})
	switch t.Kind() {
	case reflect.String:
		schema["type"] = "string"
	case reflect.Bool:
		schema["type"] = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		schema["type"] = "number"
	case reflect.Array, reflect.Slice:
		schema["type"] = "array"
		schema["items"] = createFieldSchema(t.Elem())
	case reflect.Map:
		schema["type"] = "object"
	case reflect.Struct:
		schema["type"] = "object"
		schema["properties"] = make(map[string]interface{})
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}
			jsonName := field.Name
			tagParts := strings.Split(jsonTag, ",")
			if tagParts[0] != "" {
				jsonName = tagParts[0]
			}

			fieldSchema := createFieldSchema(field.Type)
			schema["properties"].(map[string]interface{})[jsonName] = fieldSchema
		}
	case reflect.Ptr:
		return createFieldSchema(t.Elem())
	default:
		schema["type"] = "string"
	}

	return schema
}

func JSONToStruct(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func StructToJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func IsCanceled(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}
