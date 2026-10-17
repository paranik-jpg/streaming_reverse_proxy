package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

const mockConversionRate = 2.0

// MockCurrencyConversion simulates a currency conversion.
// It does not call an external API.
func MockCurrencyConversion(
	ctx context.Context,
	args json.RawMessage,
) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var input struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	}

	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("decode currency arguments: %w", err)
	}

	return map[string]any{
		"original_amount": input.Amount,
		"currency":        input.Currency,
		"converted_amount": input.Amount *
			mockConversionRate,
		"mock": true,
	}, nil
}

// MockUserLookup simulates a user database lookup.
func MockUserLookup(
	ctx context.Context,
	args json.RawMessage,
) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var input struct {
		UserID int `json:"user_id"`
	}

	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("decode user arguments: %w", err)
	}

	// A small in-memory database for verification.
	users := map[int]map[string]any{
		1: {
			"user_id": 1,
			"name":    "Alice",
			"email":   "alice@example.com",
		},
		2: {
			"user_id": 2,
			"name":    "Bob",
			"email":   "bob@example.com",
		},
	}

	user, exists := users[input.UserID]
	if !exists {
		return nil, fmt.Errorf(
			"user %d not found",
			input.UserID,
		)
	}

	return user, nil
}

// RegisterMockTools registers both mock tools.
func RegisterMockTools(r *Registry) error {
	currencySchema := `{
		"type": "object",
		"properties": {
			"amount": {
				"type": "number",
				"minimum": 0
			},
			"currency": {
				"type": "string",
				"minLength": 3
			}
		},
		"required": ["amount", "currency"],
		"additionalProperties": false
	}`

	userSchema := `{
		"type": "object",
		"properties": {
			"user_id": {
				"type": "integer",
				"minimum": 1
			}
		},
		"required": ["user_id"],
		"additionalProperties": false
	}`

	if err := r.Register(
		"mock_currency_conversion",
		"Simulates a currency conversion for a non-negative amount and currency code.",
		currencySchema,
		MockCurrencyConversion,
	); err != nil {
		return fmt.Errorf("register currency tool: %w", err)
	}

	if err := r.Register(
		"mock_user_lookup",
		"Looks up a mock user by their positive integer user ID.",
		userSchema,
		MockUserLookup,
	); err != nil {
		return fmt.Errorf("register user lookup tool: %w", err)
	}

	return nil
}

