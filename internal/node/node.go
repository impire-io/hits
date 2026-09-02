// Package node runs the hits micro service. Everything callers can do exists
// as a NATS micro endpoint here; the wire contract (subjects, payload types)
// is declared by the client package and implemented in this one.
package node

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/internal/version"
)

// Start registers the hits micro service on the given connection and returns
// the running service. Stopping it is the caller's job.
func Start(nc *nats.Conn) (micro.Service, error) {
	svc, err := micro.AddService(nc, micro.Config{
		Name:        "hits",
		Version:     version.Version,
		Description: "HITS — headless, agent-native issue tracking",
	})
	if err != nil {
		return nil, fmt.Errorf("register service: %w", err)
	}
	if err := svc.AddEndpoint("ping", micro.HandlerFunc(handlePing),
		micro.WithEndpointSubject(client.PingSubject)); err != nil {
		_ = svc.Stop()
		return nil, fmt.Errorf("add ping endpoint: %w", err)
	}
	return svc, nil
}

func handlePing(req micro.Request) {
	reply, err := json.Marshal(client.About{Name: "hits", Version: version.Version})
	if err != nil {
		_ = req.Error("500", "encode reply: "+err.Error(), nil)
		return
	}
	_ = req.Respond(reply)
}
