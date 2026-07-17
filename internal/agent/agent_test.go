package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type scriptedClient struct {
	responses []Message
	errors    []error
	calls     [][]Message
}

func (c *scriptedClient) Complete(_ context.Context, messages []Message, _ []ToolDefinition) (Message, error) {
	c.calls = append(c.calls, append([]Message(nil), messages...))
	index := len(c.calls) - 1
	if index < len(c.errors) && c.errors[index] != nil {
		return Message{}, c.errors[index]
	}
	if index >= len(c.responses) {
		return Message{}, errors.New("unexpected Complete call")
	}
	return c.responses[index], nil
}

func TestAgentMultiTurnConversation(t *testing.T) {
	client := &scriptedClient{responses: []Message{
		{Role: RoleAssistant, Content: text("first answer")},
		{Role: RoleAssistant, Content: text("second answer")},
	}}
	a, err := New(client, nil, Options{SystemPrompt: "system"})
	if err != nil {
		t.Fatal(err)
	}

	if answer, err := a.Chat(context.Background(), "first question"); err != nil || answer != "first answer" {
		t.Fatalf("first Chat() = %q, %v", answer, err)
	}
	if answer, err := a.Chat(context.Background(), "second question"); err != nil || answer != "second answer" {
		t.Fatalf("second Chat() = %q, %v", answer, err)
	}

	secondCall := client.calls[1]
	if len(secondCall) != 4 {
		t.Fatalf("second call has %d messages, want 4", len(secondCall))
	}
	if secondCall[0].Role != RoleSystem || *secondCall[1].Content != "first question" || *secondCall[2].Content != "first answer" {
		t.Fatalf("second call did not preserve prior context: %#v", secondCall)
	}
}

func TestAgentExecutesToolCall(t *testing.T) {
	client := &scriptedClient{responses: []Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: FunctionCall{
					Name:      "calculate",
					Arguments: `{"operation":"multiply","a":6,"b":7}`,
				},
			}},
		},
		{Role: RoleAssistant, Content: text("42")},
	}}
	a, err := New(client, []Tool{CalculatorTool{}}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	answer, err := a.Chat(context.Background(), "6 times 7")
	if err != nil || answer != "42" {
		t.Fatalf("Chat() = %q, %v", answer, err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("Complete calls = %d, want 2", len(client.calls))
	}
	messages := client.calls[1]
	toolMessage := messages[len(messages)-1]
	if toolMessage.Role != RoleTool || toolMessage.ToolCallID != "call-1" || toolMessage.Name != "calculate" {
		t.Fatalf("tool message metadata = %#v", toolMessage)
	}
	if toolMessage.Content == nil || *toolMessage.Content != `{"result":42}` {
		t.Fatalf("tool result = %#v", toolMessage.Content)
	}
}

func TestAgentReturnsToolErrorsToModel(t *testing.T) {
	client := &scriptedClient{responses: []Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{{
				ID: "missing-1", Type: "function",
				Function: FunctionCall{Name: "missing", Arguments: `{}`},
			}},
		},
		{Role: RoleAssistant, Content: text("recovered")},
	}}
	a, err := New(client, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}

	answer, err := a.Chat(context.Background(), "use a missing tool")
	if err != nil || answer != "recovered" {
		t.Fatalf("Chat() = %q, %v", answer, err)
	}
	toolResult := *client.calls[1][2].Content
	if !strings.Contains(toolResult, `"error"`) || !strings.Contains(toolResult, "unknown tool") {
		t.Fatalf("tool error result = %q", toolResult)
	}
}

func TestAgentRollsBackFailedTurn(t *testing.T) {
	client := &scriptedClient{
		responses: []Message{{}, {Role: RoleAssistant, Content: text("ok")}},
		errors:    []error{errors.New("network down")},
	}
	a, err := New(client, nil, Options{SystemPrompt: "system"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Chat(context.Background(), "failed question"); err == nil {
		t.Fatal("Chat() expected error")
	}
	if len(a.History()) != 1 {
		t.Fatalf("history length after failure = %d, want 1", len(a.History()))
	}
	if answer, err := a.Chat(context.Background(), "retry"); err != nil || answer != "ok" {
		t.Fatalf("retry Chat() = %q, %v", answer, err)
	}
}

func TestAgentStopsAtMaximumIterations(t *testing.T) {
	call := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{
		ID: "loop", Type: "function",
		Function: FunctionCall{Name: "calculate", Arguments: `{"operation":"add","a":1,"b":1}`},
	}}}
	client := &scriptedClient{responses: []Message{call, call}}
	a, err := New(client, []Tool{CalculatorTool{}}, Options{MaxIterations: 2})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Chat(context.Background(), "loop")
	if err == nil || !strings.Contains(err.Error(), "maximum of 2") {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(a.History()) != 0 {
		t.Fatalf("history length = %d, want rollback to 0", len(a.History()))
	}
}

func TestAgentReset(t *testing.T) {
	client := &scriptedClient{responses: []Message{{Role: RoleAssistant, Content: text("ok")}}}
	a, err := New(client, nil, Options{SystemPrompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Chat(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	a.Reset()
	history := a.History()
	if len(history) != 1 || history[0].Role != RoleSystem || *history[0].Content != "system" {
		t.Fatalf("history after reset = %#v", history)
	}
}

func TestAgentToolReceivesValidJSON(t *testing.T) {
	client := &scriptedClient{responses: []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "bad", Type: "function", Function: FunctionCall{Name: "calculate", Arguments: `{`}}}},
		{Role: RoleAssistant, Content: text("fixed")},
	}}
	a, err := New(client, []Tool{CalculatorTool{}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Chat(context.Background(), "bad args"); err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(*client.calls[1][2].Content), &result); err != nil || result["error"] == "" {
		t.Fatalf("invalid structured tool error: %v, %#v", err, result)
	}
}
