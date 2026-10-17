package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const MaxToolOutputBytes = 8 * 1024 // 8 KiB

var ErrToolOutputTooLarge = errors.New("tool output exceeds 8 KiB limit")

func (r *Registry) Execute(
	ctx context.Context,
	name string,
	args json.RawMessage,
) (any, error) {
	// 1. Lock & Lookup
	r.mu.RLock()
	tool, exists := r.tools[name]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown tool %q", name)
	}

	// Reject requests whose context has already expired.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 2. Schema Validation
	// Clean, parse, and validate in one step using schema.go utilities.
	_, err := ValidatePayload(tool.Validator, args)
	if err != nil {
		return nil, err
	}

	type handlerResult struct {
		value any
		err   error
	}

	resultCh := make(chan handlerResult, 1)

	// 3. Execute Handler in Goroutine with Context Guardrail
	go func() {
		value, err := tool.Handler(ctx, args)
		resultCh <- handlerResult{
			value: value,
			err:   err,
		}
	}()

	var result any

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case outcome := <-resultCh:
		if outcome.err != nil {
			return nil, outcome.err
		}
		result = outcome.value
	}

	// 4. Output Sanitization & Budgeting
	output, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("serialize tool output: %w", err)
	}

	// If a tool returns more than 8 Kilobytes of data,
	// reject it so the AI's prompt doesn't get flooded.
	if len(output) > MaxToolOutputBytes {
		return nil, ErrToolOutputTooLarge
	}

	return result, nil
}
