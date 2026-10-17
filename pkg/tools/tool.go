package tools

import (
	"context"
	"encoding/json"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

type Handler func(
	ctx context.Context,
	args json.RawMessage,
) (any, error)

type Tool struct {
	Name        string
	Description string
	RawSchema   json.RawMessage
	Validator   *jsonschema.Schema
	Handler     Handler
}

