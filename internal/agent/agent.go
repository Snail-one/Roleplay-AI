package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const defaultMaxIterations = 8

type Options struct {
	SystemPrompt  string
	MaxIterations int
}

type Agent struct {
	client        ChatClient
	tools         map[string]Tool
	definitions   []ToolDefinition
	systemPrompt  string
	maxIterations int
	history       []Message
}

func New(client ChatClient, tools []Tool, options Options) (*Agent, error) {
	if client == nil {
		return nil, errors.New("chat client is required")
	}

	maxIterations := options.MaxIterations
	if maxIterations == 0 {
		maxIterations = defaultMaxIterations
	}
	if maxIterations < 1 {
		return nil, errors.New("max iterations must be positive")
	}

	registered := make(map[string]Tool, len(tools))
	definitions := make([]ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			return nil, errors.New("tool cannot be nil")
		}
		definition := tool.Definition()
		name := definition.Function.Name
		if name == "" {
			return nil, errors.New("tool name cannot be empty")
		}
		if _, exists := registered[name]; exists {
			return nil, fmt.Errorf("duplicate tool %q", name)
		}
		registered[name] = tool
		definitions = append(definitions, definition)
	}

	a := &Agent{
		client:        client,
		tools:         registered,
		definitions:   definitions,
		systemPrompt:  strings.TrimSpace(options.SystemPrompt),
		maxIterations: maxIterations,
	}
	a.Reset()
	return a, nil
}

func (a *Agent) Chat(ctx context.Context, input string) (answer string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(input) == "" {
		return "", errors.New("input cannot be empty")
	}

	checkpoint := len(a.history)
	defer func() {
		if err != nil {
			a.history = a.history[:checkpoint]
		}
	}()

	a.history = append(a.history, Message{Role: RoleUser, Content: text(input)})
	for range a.maxIterations {
		response, completeErr := a.client.Complete(ctx, a.history, a.definitions)
		if completeErr != nil {
			return "", fmt.Errorf("complete chat: %w", completeErr)
		}
		if response.Role == "" {
			response.Role = RoleAssistant
		}
		if response.Role != RoleAssistant {
			return "", fmt.Errorf("unexpected response role %q", response.Role)
		}

		if len(response.ToolCalls) == 0 {
			if response.Content == nil || strings.TrimSpace(*response.Content) == "" {
				return "", errors.New("model returned an empty response")
			}
			a.history = append(a.history, response)
			return *response.Content, nil
		}

		a.history = append(a.history, response)
		for _, call := range response.ToolCalls {
			result := a.executeTool(ctx, call)
			a.history = append(a.history, Message{
				Role:       RoleTool,
				Content:    text(result),
				ToolCallID: call.ID,
				Name:       call.Function.Name,
			})
		}
	}

	return "", fmt.Errorf("agent exceeded the maximum of %d model iterations", a.maxIterations)
}

func (a *Agent) Reset() {
	a.history = a.history[:0]
	if a.systemPrompt != "" {
		a.history = append(a.history, Message{Role: RoleSystem, Content: text(a.systemPrompt)})
	}
}

func (a *Agent) History() []Message {
	return append([]Message(nil), a.history...)
}

func (a *Agent) executeTool(ctx context.Context, call ToolCall) string {
	if call.ID == "" {
		return toolError("tool call is missing an id")
	}
	if call.Type != "" && call.Type != "function" {
		return toolError(fmt.Sprintf("unsupported tool call type %q", call.Type))
	}
	tool, exists := a.tools[call.Function.Name]
	if !exists {
		return toolError(fmt.Sprintf("unknown tool %q", call.Function.Name))
	}
	result, err := tool.Execute(ctx, json.RawMessage(call.Function.Arguments))
	if err != nil {
		return toolError(err.Error())
	}
	return result
}

func toolError(message string) string {
	data, _ := json.Marshal(map[string]string{"error": message})
	return string(data)
}
