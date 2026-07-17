package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type CalculatorTool struct{}

func (CalculatorTool) Definition() ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: FunctionDefinition{
			Name:        "calculate",
			Description: "Perform basic arithmetic on two numbers.",
			Parameters: json.RawMessage(`{
                "type":"object",
                "properties":{
                    "operation":{"type":"string","enum":["add","subtract","multiply","divide"]},
                    "a":{"type":"number"},
                    "b":{"type":"number"}
                },
                "required":["operation","a","b"],
                "additionalProperties":false
            }`),
		},
	}
}

func (CalculatorTool) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args struct {
		Operation string   `json:"operation"`
		A         *float64 `json:"a"`
		B         *float64 `json:"b"`
	}
	if err := decodeStrict(arguments, &args); err != nil {
		return "", fmt.Errorf("invalid calculator arguments: %w", err)
	}
	if args.A == nil || args.B == nil {
		return "", errors.New("calculator requires both a and b")
	}

	var result float64
	switch args.Operation {
	case "add":
		result = *args.A + *args.B
	case "subtract":
		result = *args.A - *args.B
	case "multiply":
		result = *args.A * *args.B
	case "divide":
		if *args.B == 0 {
			return "", errors.New("division by zero")
		}
		result = *args.A / *args.B
	default:
		return "", fmt.Errorf("unsupported operation %q", args.Operation)
	}

	return marshalToolResult(map[string]any{"result": result})
}

type TimeTool struct {
	Now func() time.Time
}

func (TimeTool) Definition() ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: FunctionDefinition{
			Name:        "get_current_time",
			Description: "Get the current time in an optional IANA time zone such as Asia/Shanghai.",
			Parameters: json.RawMessage(`{
                "type":"object",
                "properties":{"timezone":{"type":"string","description":"Optional IANA time zone"}},
                "additionalProperties":false
            }`),
		},
	}
}

func (t TimeTool) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args struct {
		Timezone string `json:"timezone"`
	}
	if len(bytes.TrimSpace(arguments)) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if err := decodeStrict(arguments, &args); err != nil {
		return "", fmt.Errorf("invalid time arguments: %w", err)
	}

	location := time.Local
	if args.Timezone != "" {
		var err error
		location, err = time.LoadLocation(args.Timezone)
		if err != nil {
			return "", fmt.Errorf("invalid IANA time zone %q: %w", args.Timezone, err)
		}
	}

	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	current := now().In(location)
	return marshalToolResult(map[string]any{
		"current_time": current.Format(time.RFC3339),
		"timezone":     location.String(),
	})
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func marshalToolResult(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
