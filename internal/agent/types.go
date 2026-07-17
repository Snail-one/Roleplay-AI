package agent

import (
	"context"
	"encoding/json"

	"roleloom/internal/ai"
)

const (
	RoleSystem    = ai.RoleSystem
	RoleUser      = ai.RoleUser
	RoleAssistant = ai.RoleAssistant
	RoleTool      = ai.RoleTool
)

type Message = ai.Message
type ToolCall = ai.ToolCall
type FunctionCall = ai.FunctionCall
type ToolDefinition = ai.ToolDefinition
type FunctionDefinition = ai.FunctionDefinition
type ChatClient = ai.ChatClient

type Tool interface {
	Definition() ToolDefinition
	Execute(ctx context.Context, arguments json.RawMessage) (string, error)
}

func text(value string) *string {
	return &value
}
