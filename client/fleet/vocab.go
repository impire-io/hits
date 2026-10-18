package fleet

import (
	"context"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

func (c *Client) RegisterProject(ctx context.Context, r client.RegisterProjectRequest) (contract.Project, error) {
	return write[contract.Project](ctx, c, RegisterProjectSubject, r)
}

func (c *Client) AssignProject(ctx context.Context, r client.AssignProjectRequest) (contract.Project, error) {
	return write[contract.Project](ctx, c, AssignProjectSubject, r)
}

func (c *Client) RetireProject(ctx context.Context, r client.RetireProjectRequest) (contract.Project, error) {
	return write[contract.Project](ctx, c, RetireProjectSubject, r)
}

func (c *Client) ListProjects(ctx context.Context) ([]contract.Project, error) {
	return request[[]contract.Project](ctx, c, ListProjectsSubject, struct{}{})
}

func (c *Client) RegisterInitiative(ctx context.Context, r client.RegisterInitiativeRequest) (contract.Initiative, error) {
	return write[contract.Initiative](ctx, c, RegisterInitiativeSubject, r)
}

func (c *Client) RetireInitiative(ctx context.Context, r client.RetireInitiativeRequest) (contract.Initiative, error) {
	return write[contract.Initiative](ctx, c, RetireInitiativeSubject, r)
}

func (c *Client) ListInitiatives(ctx context.Context) ([]contract.Initiative, error) {
	return request[[]contract.Initiative](ctx, c, ListInitiativesSubject, struct{}{})
}

func (c *Client) RegisterRelease(ctx context.Context, r client.RegisterReleaseRequest) (contract.Release, error) {
	return write[contract.Release](ctx, c, RegisterReleaseSubject, r)
}

func (c *Client) ShipRelease(ctx context.Context, r client.ShipReleaseRequest) (contract.Release, error) {
	return write[contract.Release](ctx, c, ShipReleaseSubject, r)
}

func (c *Client) RetireRelease(ctx context.Context, r client.RetireReleaseRequest) (contract.Release, error) {
	return write[contract.Release](ctx, c, RetireReleaseSubject, r)
}

func (c *Client) ListReleases(ctx context.Context, r client.ListReleasesRequest) ([]contract.Release, error) {
	return request[[]contract.Release](ctx, c, ListReleasesSubject, r)
}
