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
