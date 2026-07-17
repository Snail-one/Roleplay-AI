package ai

import (
	"context"
	"encoding/json"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

type Message struct {
	Role             string     `json:"role"`
	Content          *string    `json:"content"`
	ReasoningContent *string    `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Backend is implemented by each model provider adapter.
type Backend interface {
	Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (Message, error)
}

type StreamEvent struct {
	Delta    string
	ToolName string
}

type EventSink func(StreamEvent) error

// StreamingBackend is implemented by providers with a native streaming protocol.
type StreamingBackend interface {
	Backend
	Stream(ctx context.Context, messages []Message, tools []ToolDefinition, sink EventSink) (Message, error)
}

// ChatClient is the provider-neutral interface consumed by the Agent.
type ChatClient = Backend
