package fleet

import (
	"context"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// RegisterProject adds a project to the vocabulary.
func (c *Client) RegisterProject(ctx context.Context, r client.RegisterProjectRequest) (contract.Project, error) {
	return write[contract.Project](ctx, c, RegisterProjectSubject, r)
}

// AssignProject moves a project to an initiative.
func (c *Client) AssignProject(ctx context.Context, r client.AssignProjectRequest) (contract.Project, error) {
	return write[contract.Project](ctx, c, AssignProjectSubject, r)
}

// RetireProject retires a project with a reason.
func (c *Client) RetireProject(ctx context.Context, r client.RetireProjectRequest) (contract.Project, error) {
	return write[contract.Project](ctx, c, RetireProjectSubject, r)
}

// ListProjects is the live projects.
func (c *Client) ListProjects(ctx context.Context) ([]contract.Project, error) {
	return request[[]contract.Project](ctx, c, ListProjectsSubject, struct{}{})
}

// RegisterInitiative adds an initiative to the vocabulary.
func (c *Client) RegisterInitiative(ctx context.Context, r client.RegisterInitiativeRequest) (contract.Initiative, error) {
	return write[contract.Initiative](ctx, c, RegisterInitiativeSubject, r)
}

// RetireInitiative retires an initiative with a reason.
func (c *Client) RetireInitiative(ctx context.Context, r client.RetireInitiativeRequest) (contract.Initiative, error) {
	return write[contract.Initiative](ctx, c, RetireInitiativeSubject, r)
}

// ListInitiatives is the live initiatives.
func (c *Client) ListInitiatives(ctx context.Context) ([]contract.Initiative, error) {
	return request[[]contract.Initiative](ctx, c, ListInitiativesSubject, struct{}{})
}

// RegisterRelease adds a release under its initiative.
func (c *Client) RegisterRelease(ctx context.Context, r client.RegisterReleaseRequest) (contract.Release, error) {
	return write[contract.Release](ctx, c, RegisterReleaseSubject, r)
}

// ShipRelease ships a release with its evidence; the node keeps the gate.
func (c *Client) ShipRelease(ctx context.Context, r client.ShipReleaseRequest) (contract.Release, error) {
	return write[contract.Release](ctx, c, ShipReleaseSubject, r)
}

// RetireRelease retires a release with a reason.
func (c *Client) RetireRelease(ctx context.Context, r client.RetireReleaseRequest) (contract.Release, error) {
	return write[contract.Release](ctx, c, RetireReleaseSubject, r)
}

// ListReleases is an initiative's live releases.
func (c *Client) ListReleases(ctx context.Context, r client.ListReleasesRequest) ([]contract.Release, error) {
	return request[[]contract.Release](ctx, c, ListReleasesSubject, r)
}
