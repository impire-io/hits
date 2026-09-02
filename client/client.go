// Package client is the Go surface for talking to a running hits service over
// NATS. Every caller — the CLI, the future MCP server, tests — goes through
// this package; there is no side door.
//
// It also declares the wire contract (subjects and payload types); the service
// in internal/node implements it, and the contract is proven by tests that run
// both ends against a real NATS server.
package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

// PingSubject is the wire subject of the service's ping endpoint.
const PingSubject = "hits.ping"

// About identifies a running hits service.
type About struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Client talks to a hits service over an established NATS connection. The
// connection's lifecycle stays with the caller.
type Client struct {
	nc *nats.Conn
}

// New wraps an established NATS connection.
func New(nc *nats.Conn) *Client {
	return &Client{nc: nc}
}

// Ping asks the running service to identify itself.
func (c *Client) Ping(ctx context.Context) (About, error) {
	msg, err := c.nc.RequestWithContext(ctx, PingSubject, nil)
	if err != nil {
		return About{}, fmt.Errorf("ping request: %w", err)
	}
	var about About
	if err := json.Unmarshal(msg.Data, &about); err != nil {
		return About{}, fmt.Errorf("decode ping reply: %w", err)
	}
	return about, nil
}
