// Package fleet speaks the wire contract of the 0.7 fleet, frozen at the
// v0.7.0 release (hits-hq decision 0023): one request and one reply per
// sentence over the subjects hits-node and the three index services
// serve, with the actor given at construction — the fleet validates a
// claimed actor, and chronicle vouches for nobody here. Nothing in this
// package follows a fleet change, because the fleet gets none; it is
// proven against the v0.7.0 binaries built from their tag
// (internal/fleettest).
package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/impire-io/hits/client"
)

// The wire subjects of the frozen contract: the node's, then the three
// index services'.
const (
	PingSubject            = "hits.ping"
	CreateSubject          = "hits.api.create"
	GetSubject             = "hits.api.get"
	EditSubject            = "hits.api.edit"
	TransitionSubject      = "hits.api.transition"
	ClaimSubject           = "hits.api.claim"
	ReleaseSubject         = "hits.api.release"
	BlockSubject           = "hits.api.block"
	UnblockSubject         = "hits.api.unblock"
	LinkSubject            = "hits.api.link"
	UnlinkSubject          = "hits.api.unlink"
	NoteSubject            = "hits.api.note"
	TombstoneSubject       = "hits.api.tombstone"
	RegisterProjectSubject = "hits.api.project.register"
	AssignProjectSubject   = "hits.api.project.assign"
	RetireProjectSubject   = "hits.api.project.retire"
	ListProjectsSubject    = "hits.api.project.list"

	RegisterInitiativeSubject = "hits.api.initiative.register"
	RetireInitiativeSubject   = "hits.api.initiative.retire"
	ListInitiativesSubject    = "hits.api.initiative.list"

	RegisterReleaseSubject = "hits.api.release.register"
	ShipReleaseSubject     = "hits.api.release.ship"
	RetireReleaseSubject   = "hits.api.release.retire"
	ListReleasesSubject    = "hits.api.release.list"

	SearchSubject         = "hits.search.query"
	SemanticSubject       = "hits.semantic.query"
	GraphNeighborsSubject = "hits.graph.neighbors"
	GraphWalkSubject      = "hits.graph.walk"
)

// CodeUnreachable is the refusal when no service answers a subject: the
// fleet is not running, or the context reaches the wrong NATS.
const CodeUnreachable = "unreachable"

// Client talks to a 0.7 fleet over an established NATS connection, as
// the actor it was given. The connection's lifecycle stays with the
// caller.
type Client struct {
	nc    *nats.Conn
	actor string
}

var _ client.Client = (*Client)(nil)

// New wraps a connection; every write carries actor.
func New(nc *nats.Conn, actor string) *Client {
	return &Client{nc: nc, actor: actor}
}

// Author is the actor every write of this client carries.
func (c *Client) Author() string { return c.actor }

// Ping asks the node to identify itself.
func (c *Client) Ping(ctx context.Context) (client.About, error) {
	return request[client.About](ctx, c, PingSubject, nil)
}

// withActor is the write's wire form: the request's fields and the actor
// beside them, as the 0.7 endpoints read them.
func (c *Client) withActor(r any) (map[string]any, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	m["actor"] = c.actor
	return m, nil
}

// write round-trips one write endpoint as the client's actor.
func write[T any](ctx context.Context, c *Client, subject string, r any) (T, error) {
	var zero T
	body, err := c.withActor(r)
	if err != nil {
		return zero, err
	}
	return request[T](ctx, c, subject, body)
}

// request round-trips one endpoint call, surfacing the service's
// rejections as *client.APIError. A nil request sends no body.
func request[T any](ctx context.Context, c *Client, subject string, req any) (T, error) {
	var zero T
	var data []byte
	if req != nil {
		var err error
		if data, err = json.Marshal(req); err != nil {
			return zero, fmt.Errorf("encode request: %w", err)
		}
	}
	msg, err := c.nc.RequestWithContext(ctx, subject, data)
	if err != nil {
		if errors.Is(err, nats.ErrNoResponders) {
			return zero, &client.APIError{Code: CodeUnreachable, Message: "no service answers " + subject + ": is the 0.7 fleet running on this context?"}
		}
		return zero, fmt.Errorf("request %s: %w", subject, err)
	}
	if code := msg.Header.Get(micro.ErrorCodeHeader); code != "" {
		return zero, &client.APIError{Code: code, Message: msg.Header.Get(micro.ErrorHeader)}
	}
	var out T
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		return zero, fmt.Errorf("decode %s reply: %w", subject, err)
	}
	return out, nil
}
