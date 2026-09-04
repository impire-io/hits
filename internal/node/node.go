// Package node runs the hits micro service: the single writer of the
// ops-log, the state projections folded from it, and the hits.api endpoint
// surface. The wire contract (subjects, payload types) is declared by the
// client package and implemented in this one.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/internal/version"
)

// Start ensures the ops-log stream and the state bucket exist (with the
// byte budgets cfg carries), heals the projection by replay — counter
// included — and registers the hits micro service on the given connection.
// Stopping the returned service is the caller's job.
func Start(ctx context.Context, nc *nats.Conn, cfg Config) (micro.Service, error) {
	st, err := openStore(ctx, nc, cfg)
	if err != nil {
		return nil, err
	}
	if err := st.replay(ctx); err != nil {
		return nil, fmt.Errorf("replay ops-log: %w", err)
	}
	h := &handlers{st: st}

	svc, err := micro.AddService(nc, micro.Config{
		Name:        "hits",
		Version:     version.Version,
		Description: "HITS — headless, agent-native issue tracking",
	})
	if err != nil {
		return nil, fmt.Errorf("register service: %w", err)
	}
	endpoints := []struct {
		name    string
		subject string
		handler micro.HandlerFunc
	}{
		{"ping", client.PingSubject, handlePing},
		{"create", client.CreateSubject, h.create},
		{"get", client.GetSubject, h.get},
		{"edit", client.EditSubject, h.edit},
		{"transition", client.TransitionSubject, h.transition},
		{"claim", client.ClaimSubject, h.claim},
		{"release", client.ReleaseSubject, h.release},
		{"block", client.BlockSubject, h.block},
		{"unblock", client.UnblockSubject, h.unblock},
		{"link", client.LinkSubject, h.link},
		{"unlink", client.UnlinkSubject, h.unlink},
		{"note", client.NoteSubject, h.note},
		{"tombstone", client.TombstoneSubject, h.tombstone},
		{"project-register", client.RegisterProjectSubject, h.registerProject},
		{"project-list", client.ListProjectsSubject, h.listProjects},
	}
	for _, e := range endpoints {
		if err := svc.AddEndpoint(e.name, e.handler, micro.WithEndpointSubject(e.subject)); err != nil {
			_ = svc.Stop()
			return nil, fmt.Errorf("add %s endpoint: %w", e.name, err)
		}
	}
	// Flush so the endpoint subscriptions have reached the server: once
	// Start returns, a request from any connection must find a responder.
	if err := nc.FlushTimeout(5 * time.Second); err != nil {
		_ = svc.Stop()
		return nil, fmt.Errorf("flush endpoint subscriptions: %w", err)
	}
	return svc, nil
}

func handlePing(req micro.Request) {
	reply, err := json.Marshal(client.About{Name: "hits", Version: version.Version})
	if err != nil {
		_ = req.Error("internal", "encode reply: "+err.Error(), nil)
		return
	}
	_ = req.Respond(reply)
}
