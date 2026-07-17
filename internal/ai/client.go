package ai

import (
	"context"
	"errors"
)

// Client is the single provider-neutral calling layer used by the application.
// Provider selection and wire-protocol details stay behind its Backend.
type Client struct {
	backend Backend
}

func NewClient(backend Backend) (*Client, error) {
	if backend == nil {
		return nil, errors.New("AI backend is required")
	}
	return &Client{backend: backend}, nil
}

func (c *Client) Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (Message, error) {
	return c.backend.Complete(ctx, messages, tools)
}

func (c *Client) Stream(ctx context.Context, messages []Message, tools []ToolDefinition, sink EventSink) (Message, error) {
	if streaming, ok := c.backend.(StreamingBackend); ok {
		return streaming.Stream(ctx, messages, tools, sink)
	}
	message, err := c.backend.Complete(ctx, messages, tools)
	if err == nil && message.Content != nil && sink != nil {
		err = sink(StreamEvent{Delta: *message.Content})
	}
	return message, err
}
