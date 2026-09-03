// Command hits-mcp is the HITS MCP server: the agent action surface.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/impire-io/hits/internal/mcp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcp.Run(ctx, os.Args[1:], os.Stderr, mcp.ContextConnector); err != nil {
		fmt.Fprintln(os.Stderr, "hits-mcp:", err)
		os.Exit(1)
	}
}
