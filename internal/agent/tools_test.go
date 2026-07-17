package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCalculatorTool(t *testing.T) {
	tests := []struct {
		name      string
		arguments string
		want      float64
		wantError string
	}{
		{name: "add", arguments: `{"operation":"add","a":2,"b":3}`, want: 5},
		{name: "subtract", arguments: `{"operation":"subtract","a":2,"b":3}`, want: -1},
		{name: "multiply", arguments: `{"operation":"multiply","a":2.5,"b":4}`, want: 10},
		{name: "divide", arguments: `{"operation":"divide","a":9,"b":2}`, want: 4.5},
		{name: "divide by zero", arguments: `{"operation":"divide","a":9,"b":0}`, wantError: "division by zero"},
		{name: "unknown operation", arguments: `{"operation":"power","a":2,"b":3}`, wantError: "unsupported operation"},
		{name: "missing operand", arguments: `{"operation":"add","a":2}`, wantError: "requires both"},
		{name: "unknown field", arguments: `{"operation":"add","a":2,"b":3,"c":4}`, wantError: "unknown field"},
		{name: "invalid JSON", arguments: `{`, wantError: "invalid calculator arguments"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := (CalculatorTool{}).Execute(context.Background(), json.RawMessage(test.arguments))
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Execute() error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			var decoded struct {
				Result float64 `json:"result"`
			}
			if err := json.Unmarshal([]byte(result), &decoded); err != nil {
				t.Fatalf("invalid result JSON: %v", err)
			}
			if decoded.Result != test.want {
				t.Fatalf("result = %v, want %v", decoded.Result, test.want)
			}
		})
	}
}

func TestTimeTool(t *testing.T) {
	fixed := time.Date(2026, time.July, 17, 12, 30, 0, 0, time.UTC)
	tool := TimeTool{Now: func() time.Time { return fixed }}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"Asia/Shanghai"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var decoded struct {
		CurrentTime string `json:"current_time"`
		Timezone    string `json:"timezone"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}
	if decoded.CurrentTime != "2026-07-17T20:30:00+08:00" {
		t.Fatalf("current_time = %q", decoded.CurrentTime)
	}
	if decoded.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q", decoded.Timezone)
	}
}

func TestTimeToolRejectsInvalidTimezone(t *testing.T) {
	_, err := (TimeTool{}).Execute(context.Background(), json.RawMessage(`{"timezone":"Mars/Olympus"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid IANA time zone") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestToolsRespectCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (CalculatorTool{}).Execute(ctx, json.RawMessage(`{"operation":"add","a":1,"b":2}`)); err == nil {
		t.Fatal("CalculatorTool.Execute() expected canceled context error")
	}
	if _, err := (TimeTool{}).Execute(ctx, json.RawMessage(`{}`)); err == nil {
		t.Fatal("TimeTool.Execute() expected canceled context error")
	}
}
