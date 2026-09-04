package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/impire-io/hits/internal/connect"
)

// runAuth is the IDP verb family (hits-hq 02-DESIGN/idp-auth.md § the
// verbs). The verbs are thin: resolution, the grant, and the cache all
// live in internal/connect; login is the one interactive moment anywhere
// in the client.
func runAuth(inv *invocation) error {
	if len(inv.args) == 0 {
		return errors.New(`auth: want "login", "status" or "logout"`)
	}
	sub, rest := inv.args[0], inv.args[1:]
	switch sub {
	case "login":
		return runAuthLogin(inv, rest)
	case "status":
		return runAuthStatus(inv, rest)
	case "logout":
		return runAuthLogout(inv, rest)
	default:
		return fmt.Errorf(`auth: unknown subcommand %q, want "login", "status" or "logout"`, sub)
	}
}

// Each verb takes --context after the subcommand too — the design writes
// `hits auth login --context <name>`; the global flag is the default.
const authContextUsage = "hits context to authenticate (default: the global --context)"

func runAuthLogin(inv *invocation, args []string) error {
	fs := inv.flagSet("auth login", "auth login [--context <name>]")
	name := fs.String("context", inv.contextName, authContextUsage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	return connect.Login(inv.ctx, *name, inv.out)
}

func runAuthStatus(inv *invocation, args []string) error {
	fs := inv.flagSet("auth status", "auth status [--context <name>]")
	name := fs.String("context", inv.contextName, authContextUsage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	st, err := connect.Status(*name)
	if err != nil {
		return err
	}
	if inv.json {
		return emit(inv.out, st)
	}
	fmt.Fprintf(inv.out, "context  %s (%s)\n", st.Context, st.Path)
	if st.NoCache {
		fmt.Fprintf(inv.out, "not logged in: run 'hits auth login --context %s'\n", st.Context)
		return nil
	}
	if st.Subject != "" {
		fmt.Fprintf(inv.out, "subject  %s\n", st.Subject)
	}
	if st.Expiry.IsZero() {
		fmt.Fprintln(inv.out, "expires  never declared")
	} else {
		fmt.Fprintf(inv.out, "expires  %s (%s)\n", st.Expiry.Format(time.RFC3339), until(st.Expiry))
	}
	fmt.Fprintf(inv.out, "refresh  %v\n", st.HasRefresh)
	return nil
}

func runAuthLogout(inv *invocation, args []string) error {
	fs := inv.flagSet("auth logout", "auth logout [--context <name>]")
	name := fs.String("context", inv.contextName, authContextUsage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	resolved, err := connect.Logout(*name)
	if err != nil {
		return err
	}
	fmt.Fprintf(inv.out, "logged out: context %q (nothing is revoked at the IDP)\n", resolved)
	return nil
}

func until(t time.Time) string {
	d := time.Until(t)
	if d < 0 {
		return "expired"
	}
	return "in " + d.Round(time.Second).String()
}
