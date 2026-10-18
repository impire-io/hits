package client

import (
	"github.com/impire-io/hits/contract"
)

// RegisterProjectRequest registers a project into an initiative.
type RegisterProjectRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Initiative  string `json:"initiative"`
}

// AssignProjectRequest moves a project to an initiative, or backfills one.
type AssignProjectRequest struct {
	Slug       string `json:"slug"`
	Initiative string `json:"initiative"`
}

// RetireProjectRequest retires a project, reason required.
type RetireProjectRequest struct {
	Slug   string `json:"slug"`
	Reason string `json:"reason"`
}

// RegisterInitiativeRequest registers an initiative.
type RegisterInitiativeRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// RetireInitiativeRequest retires an initiative, reason required.
type RetireInitiativeRequest struct {
	Slug   string `json:"slug"`
	Reason string `json:"reason"`
}

// RegisterReleaseRequest registers a release of an initiative.
type RegisterReleaseRequest struct {
	Initiative  string `json:"initiative"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ShipReleaseRequest ships a release with its verifiable refs.
type ShipReleaseRequest struct {
	Initiative string             `json:"initiative"`
	Slug       string             `json:"slug"`
	Refs       []contract.ShipRef `json:"refs"`
	Note       string             `json:"note,omitempty"`
}

// RetireReleaseRequest retires a release, reason required.
type RetireReleaseRequest struct {
	Initiative string `json:"initiative"`
	Slug       string `json:"slug"`
	Reason     string `json:"reason"`
}

// ListReleasesRequest lists one initiative's releases.
type ListReleasesRequest struct {
	Initiative string `json:"initiative"`
}
