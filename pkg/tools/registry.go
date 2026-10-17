package tools

import (
	"encoding/json"
	"fmt"
	"sync"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]*Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*Tool),
	}
}

func (r *Registry) Register(
	name string,
	description string,
	schema string,
	handler Handler,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reject duplicate tool names.
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}

	// Compile the schema once, during registration.
	validator, err := CompileToolSchema(schema)
	if err != nil {
		return fmt.Errorf(
			"compile schema for tool %q: %w",
			name,
			err,
		)
	}

	// Store the original schema and compiled metadata.
	r.tools[name] = &Tool{
		Name:        name,
		Description: description,
		RawSchema:   json.RawMessage(schema),
		Validator:   validator,
		Handler:     handler,
	}

	return nil
}

