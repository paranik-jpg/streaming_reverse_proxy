package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"streaming_reverse_proxy/pkg/engine"
	"streaming_reverse_proxy/pkg/tools"
)

func TestEngineSelfCorrectsInvalidToolArguments(t *testing.T) {
	registry := tools.NewRegistry()
	if err := tools.RegisterMockTools(registry); err != nil {
		t.Fatalf("register mock tools: %v", err)
	}

	turn := 0

	// Simulate the upstream /api/chat endpoint.
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}

			if r.URL.Path != "/" && r.URL.Path != "/api/chat" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}

			var request engine.ChatRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode request: %v", err)
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}

			turn++

			switch turn {
			case 1:
				// Turn 1: Invalid user_id violates minimum: 1.
				if len(request.Tools) == 0 {
					t.Error("expected tools to be available on turn 1")
				}

				// The Fake AI replies:
				// "Okay, I want to call mock_user_lookup with user_id: -5."
				writeChatResponse(t, w, engine.ChatResponse{
					Message: engine.Message{
						Role: "assistant",
						ToolCalls: []engine.ToolCall{
							{
								ID:   "call-invalid",
								Type: "function",
								Function: engine.ToolCallFunction{
									Name: "mock_user_lookup",
									Arguments: json.RawMessage(
										`{"user_id": -5}`,
									),
								},
							},
						},
					},
				})

			case 2:
				// The correction prompt must be in the conversation.
				foundFeedback := false

				for _, message := range request.Messages {
					if message.Role == "user" &&
						strings.Contains(
							message.Content,
							"failed schema validation",
						) &&
						strings.Contains(
							message.Content,
							"user_id",
						) {
						foundFeedback = true
						break
					}
				}

				if !foundFeedback {
					t.Errorf(
						"turn 2 is missing corrective validation feedback; messages: %+v",
						request.Messages,
					)
				}

				// Turn 2: The model corrects the arguments.
				// The Fake AI realizes its mistake and replies:
				// "My apologies! Let me run mock_user_lookup with user_id: 1 instead."
				writeChatResponse(t, w, engine.ChatResponse{
					Message: engine.Message{
						Role: "assistant",
						ToolCalls: []engine.ToolCall{
							{
								ID:   "call-valid",
								Type: "function",
								Function: engine.ToolCallFunction{
									Name: "mock_user_lookup",
									Arguments: json.RawMessage(
										`{"user_id": 1}`,
									),
								},
							},
						},
					},
				})

			case 3:
				// The successful tool result must be in the history.
				foundToolResult := false
				for _, message := range request.Messages {
					if message.Role == "tool" &&
						message.ToolCallID == "call-valid" &&
						strings.Contains(message.Content, "Alice") {
						foundToolResult = true
						break
					}
				}

				if !foundToolResult {
					t.Error("turn 3 is missing the successful tool result")
				}

				// The Fake AI writes the final answer:
				// "Found Alice. Her user ID is 1."
				writeChatResponse(t, w, engine.ChatResponse{
					Message: engine.Message{
						Role:    "assistant",
						Content: "Found Alice. Her user ID is 1.",
					},
				})

			default:
				t.Errorf("unexpected upstream turn %d", turn)
				http.Error(w, "unexpected turn", http.StatusInternalServerError)
			}
		},
	))
	defer server.Close()

	eng := engine.NewEngine(
		"mock-model",
		&engine.HTTPUpstream{
			URL:    server.URL + "/api/chat",
			Client: server.Client(),
		},
		registry,
		[]engine.ToolDeclaration{
			{
				Type: "function",
				Function: engine.ToolFunctionSchema{
					Name:        "mock_user_lookup",
					Description: "Looks up a user by positive integer ID.",
					Parameters: json.RawMessage(`{
						"type": "object",
						"properties": {
							"user_id": {
								"type": "integer",
								"minimum": 1
							}
						},
						"required": ["user_id"],
						"additionalProperties": false
					}`),
				},
			},
		},
	)

	// Our engine says to the AI:
	// "Here is a prompt: 'Look up user 1'".
	answer, err := eng.Run(
		context.Background(),
		[]engine.Message{
			{
				Role:    "user",
				Content: "Look up user 1.",
			},
		},
	)

	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if answer != "Found Alice. Her user ID is 1." {
		t.Fatalf("answer = %q, want final synthesized answer", answer)
	}

	if turn != 3 {
		t.Fatalf("upstream turns = %d, want 3", turn)
	}
}

func writeChatResponse(
	t *testing.T,
	w http.ResponseWriter,
	response engine.ChatResponse,
) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Errorf("encode simulated upstream response: %v", err)
	}
}
