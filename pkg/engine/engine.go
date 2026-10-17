package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"streaming_reverse_proxy/pkg/tools"
)

const (
	MaxRetries   = 2
	MaxCallDepth = 3
)

var ErrRetriesExhausted = errors.New(
	"tool argument correction retries exhausted",
)

var ErrCallDepthExceeded = errors.New(
	"tool call depth limit reached",
)

// Message represents one entry in the model conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDeclaration describes a tool available to the model.
type ToolDeclaration struct {
	Type     string             `json:"type"`
	Function ToolFunctionSchema `json:"function"`
}

type ToolFunctionSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ChatRequest struct {
	Model    string            `json:"model"`
	Messages []Message         `json:"messages"`
	Tools    []ToolDeclaration `json:"tools,omitempty"`
	Stream   bool              `json:"stream"`
}

type ChatResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done,omitempty"`
}

// UpstreamClient separates model communication from orchestration.
type UpstreamClient interface {
	Chat(ctx context.Context, request ChatRequest) (ChatResponse, error)
}

// HTTPUpstream sends non-streaming requests to the upstream /api/chat endpoint.
type HTTPUpstream struct {
	URL    string
	Client *http.Client
}

func (u *HTTPUpstream) Chat(ctx context.Context, request ChatRequest) (ChatResponse, error) {
	var response ChatResponse

	body, err := json.Marshal(request)
	if err != nil {
		return response, fmt.Errorf("encode upstream request: %w", err)
	}

	client := u.Client
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		u.URL,
		bytes.NewReader(body),
	)
	if err != nil {
		return response, fmt.Errorf("create upstream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return response, fmt.Errorf("call upstream model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		details, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return response, fmt.Errorf(
			"upstream returned HTTP %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(details)),
		)
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return response, fmt.Errorf("decode upstream response: %w", err)
	}

	return response, nil
}

// Engine coordinates the model, conversation history, and tool registry.
// Create one Engine per conversation/request.
type Engine struct {
	Model    string
	Upstream UpstreamClient
	Registry *tools.Registry
	Tools    []ToolDeclaration

	messages   []Message
	retryCount int
	callDepth  int
}

func NewEngine(
	model string,
	upstream UpstreamClient,
	registry *tools.Registry,
	toolDecls []ToolDeclaration,
) *Engine {
	return &Engine{
		Model:    model,
		Upstream: upstream,
		Registry: registry,
		Tools:    toolDecls,
	}
}

// Run processes the conversation until the model returns a text answer
// or a terminal error occurs.
func (e *Engine) Run(ctx context.Context, initialMessages []Message) (string, error) {
	if e.Upstream == nil {
		return "", errors.New("upstream client is required")
	}
	if e.Registry == nil {
		return "", errors.New("tool registry is required")
	}

	e.messages = append([]Message(nil), initialMessages...)
	e.retryCount = 0
	e.callDepth = 0

InferenceLoop:
	for {
		// At the depth limit, make one final request with tools disabled.
		activeTools := e.Tools
		if e.callDepth >= MaxCallDepth {
			activeTools = nil
		}

		response, err := e.Upstream.Chat(ctx, ChatRequest{
			Model:    e.Model,
			Messages: e.messages,
			Tools:    activeTools,
			Stream:   false,
		})
		if err != nil {
			return "", err
		}

		assistantMessage := response.Message
		assistantMessage.Role = "assistant"

		// No tool calls means the model has produced its final answer.
		if len(assistantMessage.ToolCalls) == 0 {
			e.messages = append(e.messages, assistantMessage)
			return assistantMessage.Content, nil
		}

		// The final synthesis request must not start another tool chain.
		if e.callDepth >= MaxCallDepth {
			return "", ErrCallDepthExceeded
		}

		// Preserve the full assistant message, including tool-call metadata.
		e.messages = append(e.messages, assistantMessage)

		for i, call := range assistantMessage.ToolCalls {
			if call.ID == "" {
				call.ID = fmt.Sprintf("call-%d-%d", e.callDepth, i)
			}

			result, err := e.Registry.Execute(
				ctx,
				call.Function.Name,
				call.Function.Arguments,
			)
			if err != nil {
				validationDetails := strings.Join(
					tools.ExtractValidationErrors(err),
					"; ",
				)

				if e.retryCount >= MaxRetries {
					return "", fmt.Errorf(
						"%w: %s",
						ErrRetriesExhausted,
						validationDetails,
					)
				}

				feedback := fmt.Sprintf(
					"Your tool call arguments failed schema validation with error: %s. "+
						"Provide the corrected arguments conforming strictly to the defined schema.",
					validationDetails,
				)

				e.messages = append(e.messages, Message{
					Role:    "user",
					Content: feedback,
				})

				e.retryCount++

				// Retry inference with the expanded conversation history.
				continue InferenceLoop
			}

			// Encode the result as text for the model's tool message.
			output, err := json.Marshal(result)
			if err != nil {
				return "", fmt.Errorf("encode tool result: %w", err)
			}

			e.messages = append(e.messages, Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    string(output),
			})
		}

		// A successful tool round resets the correction budget.
		e.retryCount = 0
		e.callDepth++
	}
}

