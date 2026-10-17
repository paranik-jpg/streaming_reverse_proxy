package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// CompileToolSchema compiles a JSON Schema from an in-memory string.
func CompileToolSchema(rawSchemaJSON string) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()

	err := compiler.AddResource(
		"tool_schema.json",
		strings.NewReader(rawSchemaJSON),
	)
	if err != nil {
		return nil, err
	}

	schema, err := compiler.Compile("tool_schema.json")
	if err != nil {
		return nil, err
	}

	return schema, nil
}

// SanitizePayload removes surrounding whitespace and optional Markdown fences.
func SanitizePayload(raw string) string {
	raw = strings.TrimSpace(raw)

	// Strip an opening Markdown fence, optionally labelled "json".
	if strings.HasPrefix(raw, "```") {
		newline := strings.IndexByte(raw, '\n')
		if newline != -1 {
			label := strings.TrimSpace(raw[3:newline])
			if label == "" || strings.EqualFold(label, "json") {
				raw = raw[newline+1:]
			}
		}
	}

	// Strip a closing Markdown fence.
	raw = strings.TrimSpace(raw)
	if strings.HasSuffix(raw, "```") {
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "```"))
	}

	return strings.TrimSpace(raw)
}

// DecodePayload converts raw JSON bytes into a generic Go value.
func DecodePayload(rawBytes []byte) (any, error) {
	cleaned := SanitizePayload(string(rawBytes))

	var payload any

	if err := json.Unmarshal([]byte(cleaned), &payload); err != nil {
		return nil, fmt.Errorf("invalid JSON syntax: %w", err)
	}

	return payload, nil
}

func ValidatePayload(schema *jsonschema.Schema, rawBytes []byte) (any, error) {
	payload, err := DecodePayload(rawBytes)
	if err != nil {
		return nil, err
	}

	err = schema.Validate(payload)
	if err != nil {
		return nil, fmt.Errorf(
			"payload violates schema: %s",
			strings.Join(ExtractValidationErrors(err), "; "),
		)
	}

	return payload, nil
}

func ExtractValidationErrors(err error) []string {
	if err == nil {
		return nil
	}

	valErr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []string{err.Error()}
	}

	var messages []string

	var walk func(*jsonschema.ValidationError)
	walk = func(current *jsonschema.ValidationError) {
		if len(current.Causes) == 0 {
			location := current.InstanceLocation
			if location == "" {
				location = "/"
			}

			messages = append(
				messages,
				fmt.Sprintf("%s: %s", location, current.Message),
			)
			return
		}

		for _, cause := range current.Causes {
			walk(cause)
		}
	}

	walk(valErr)
	return messages
}

