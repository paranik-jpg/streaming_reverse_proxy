package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"streaming_reverse_proxy/pkg/tools"
)

func TestMockTools(t *testing.T) {
	registry := tools.NewRegistry()

	if err := tools.RegisterMockTools(registry); err != nil {
		t.Fatalf("register mock tools: %v", err)
	}

	tests := []struct {
		name    string
		args    string
		wantErr bool
	}{
		{
			name: "mock_currency_conversion",
			args: `{"amount":100,"currency":"USD"}`,
		},
		{
			name: "mock_user_lookup",
			args: `{"user_id":1}`,
		},
		{
			name:    "invalid currency amount",
			args:    `{"amount":"100","currency":"USD"}`,
			wantErr: true,
		},
		{
			name:    "invalid user ID",
			args:    `{"user_id":1.5}`,
			wantErr: true,
		},
		{
			name:    "unknown tool",
			args:    `{}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.Execute(
				context.Background(),
				tt.name,
				json.RawMessage(tt.args),
			)

			if (err != nil) != tt.wantErr {
				t.Fatalf(
					"Execute() error = %v, wantErr %v",
					err,
					tt.wantErr,
				)
			}

			if err == nil && result == nil {
				t.Fatal("expected a non-nil result")
			}
		})
	}
}

