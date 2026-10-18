package chronicle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	chron "github.com/impire-io/chronicle/client"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// The three registered vocabularies — projects, initiatives, releases —
// written with the same loop as an item and listed from the store's
// state.

func jsonDecode(raw []byte, into any) error { return json.Unmarshal(raw, into) }

func errorsAs(err error, target any) bool { return errors.As(err, target) }

func isExists(err error) bool { return errors.Is(err, chron.ErrInstanceExists) }

// --- projects ------------------------------------------------------------------

func (c *Client) loadProject(ctx context.Context, slug string) (*contract.Project, head, error) {
	h, err := c.head(ctx, contract.ProjectPath(slug))
	if err != nil || !h.exists {
		return nil, h, err
	}
	p, err := contract.ProjectFromState(slug, h.state)
	if err != nil {
		return nil, h, err
	}
	p.Seq = h.stateSeq
	return p, h, nil
}

func (c *Client) execProject(ctx context.Context, slug string, op contract.Op) (contract.Project, error) {
	for attempt := 0; ; attempt++ {
		current, h, err := c.loadProject(ctx, slug)
		if err != nil {
			return contract.Project{}, refusal(err)
		}
		if err := contract.CheckProjectOp(current, op); err != nil {
			return contract.Project{}, refusal(err)
		}
		next, err := contract.ApplyProject(current, op, h.last+1)
		if err != nil {
			return contract.Project{}, refusal(err)
		}
		opType, err := contract.VocabOperation(op)
		if err != nil {
			return contract.Project{}, err
		}
		w := write{path: contract.ProjectPath(slug), opType: opType, opID: op.ID, expect: h.last}
		if op.Op == contract.OpRegistered {
			w.create, w.data = true, contract.ProjectState(next)
		} else {
			w.data = contract.Diff(contract.ProjectState(current), contract.ProjectState(next))
		}
		err = c.send(ctx, w)
		switch {
		case w.create && isExists(err):
			return contract.Project{}, &client.APIError{Code: "already-registered", Message: fmt.Sprintf("project %q is already registered", slug)}
		case moved(err) && attempt < maxRetries:
			continue
		case err != nil:
			return contract.Project{}, refusal(err)
		}
		return *next, nil
	}
}

// RegisterProject registers a project into a live initiative; a second
// registration of the slug is refused by the create-only guard.
func (c *Client) RegisterProject(ctx context.Context, r client.RegisterProjectRequest) (contract.Project, error) {
	op, err := contract.NewOp(contract.OpRegistered, r.Slug, c.Author(), contract.RegisteredPayload{Name: r.Name, Description: r.Description, Initiative: r.Initiative})
	if err != nil {
		return contract.Project{}, err
	}
	if r.Initiative != "" {
		if err := c.checkInitiativeLive(ctx, r.Initiative); err != nil {
			return contract.Project{}, refusal(err)
		}
	}
	return c.execProject(ctx, r.Slug, op)
}

// AssignProject moves a project to a live initiative.
func (c *Client) AssignProject(ctx context.Context, r client.AssignProjectRequest) (contract.Project, error) {
	if err := c.checkInitiativeLive(ctx, r.Initiative); err != nil {
		return contract.Project{}, refusal(err)
	}
	op, err := contract.NewOp(contract.OpAssigned, r.Slug, c.Author(), contract.AssignedPayload{Initiative: r.Initiative})
	if err != nil {
		return contract.Project{}, err
	}
	return c.execProject(ctx, r.Slug, op)
}

// RetireProject retires a project; the slug leaves the vocabulary.
func (c *Client) RetireProject(ctx context.Context, r client.RetireProjectRequest) (contract.Project, error) {
	op, err := contract.NewOp(contract.OpRetired, r.Slug, c.Author(), contract.RetiredPayload{Reason: r.Reason})
	if err != nil {
		return contract.Project{}, err
	}
	return c.execProject(ctx, r.Slug, op)
}

// ListProjects lists the registered, unretired projects.
func (c *Client) ListProjects(ctx context.Context) ([]contract.Project, error) {
	var projects []contract.Project
	for info, err := range c.cc.ListInstances(ctx, c.store, chron.ByType(contract.TypeProject)) {
		if err != nil {
			return nil, refusal(err)
		}
		_, slug := contract.ParsePath(info.Path)
		p, err := contract.ProjectFromState(slug, info.State)
		if err != nil {
			return nil, err
		}
		if p.Retired {
			continue
		}
		p.Seq = info.Seq
		projects = append(projects, *p)
	}
	sort.Slice(projects, func(a, b int) bool { return projects[a].Slug < projects[b].Slug })
	return projects, nil
}

// --- initiatives ---------------------------------------------------------------

func (c *Client) loadInitiative(ctx context.Context, slug string) (*contract.Initiative, head, error) {
	h, err := c.head(ctx, contract.InitiativePath(slug))
	if err != nil || !h.exists {
		return nil, h, err
	}
	i, _, err := contract.InitiativeFromState(slug, h.state)
	if err != nil {
		return nil, h, err
	}
	i.Seq = h.stateSeq
	return i, h, nil
}

func (c *Client) execInitiative(ctx context.Context, slug string, op contract.Op) (contract.Initiative, error) {
	for attempt := 0; ; attempt++ {
		current, h, err := c.loadInitiative(ctx, slug)
		if err != nil {
			return contract.Initiative{}, refusal(err)
		}
		if err := contract.CheckInitiativeOp(current, op); err != nil {
			return contract.Initiative{}, refusal(err)
		}
		next, err := contract.ApplyInitiative(current, op, h.last+1)
		if err != nil {
			return contract.Initiative{}, refusal(err)
		}
		opType, err := contract.VocabOperation(op)
		if err != nil {
			return contract.Initiative{}, err
		}
		w := write{path: contract.InitiativePath(slug), opType: opType, opID: op.ID, expect: h.last}
		if op.Op == contract.OpRegistered {
			state := contract.InitiativeState(next)
			state["next"] = 1 // the counter is born with the initiative
			w.create, w.data = true, state
		} else {
			w.data = contract.Diff(contract.InitiativeState(current), contract.InitiativeState(next))
		}
		err = c.send(ctx, w)
		switch {
		case w.create && isExists(err):
			return contract.Initiative{}, &client.APIError{Code: "already-registered", Message: fmt.Sprintf("initiative %q is already registered", slug)}
		case moved(err) && attempt < maxRetries:
			continue
		case err != nil:
			return contract.Initiative{}, refusal(err)
		}
		return *next, nil
	}
}

// RegisterInitiative registers an initiative, its counter born at 1.
func (c *Client) RegisterInitiative(ctx context.Context, r client.RegisterInitiativeRequest) (contract.Initiative, error) {
	op, err := contract.NewOp(contract.OpRegistered, r.Slug, c.Author(), contract.RegisteredPayload{Name: r.Name, Description: r.Description})
	if err != nil {
		return contract.Initiative{}, err
	}
	return c.execInitiative(ctx, r.Slug, op)
}

// RetireInitiative retires an initiative.
func (c *Client) RetireInitiative(ctx context.Context, r client.RetireInitiativeRequest) (contract.Initiative, error) {
	op, err := contract.NewOp(contract.OpRetired, r.Slug, c.Author(), contract.RetiredPayload{Reason: r.Reason})
	if err != nil {
		return contract.Initiative{}, err
	}
	return c.execInitiative(ctx, r.Slug, op)
}

// ListInitiatives lists the registered, unretired initiatives.
func (c *Client) ListInitiatives(ctx context.Context) ([]contract.Initiative, error) {
	var initiatives []contract.Initiative
	for info, err := range c.cc.ListInstances(ctx, c.store, chron.ByType(contract.TypeInitiative)) {
		if err != nil {
			return nil, refusal(err)
		}
		_, slug := contract.ParsePath(info.Path)
		i, _, err := contract.InitiativeFromState(slug, info.State)
		if err != nil {
			return nil, err
		}
		if i.Retired {
			continue
		}
		i.Seq = info.Seq
		initiatives = append(initiatives, *i)
	}
	sort.Slice(initiatives, func(a, b int) bool { return initiatives[a].Slug < initiatives[b].Slug })
	return initiatives, nil
}

// --- releases ------------------------------------------------------------------

func (c *Client) loadRelease(ctx context.Context, initiative, slug string) (*contract.Release, head, error) {
	h, err := c.head(ctx, contract.ReleasePath(initiative, slug))
	if err != nil || !h.exists {
		return nil, h, err
	}
	r, err := contract.ReleaseFromState(initiative, h.state)
	if err != nil {
		return nil, h, err
	}
	r.Seq = h.stateSeq
	return r, h, nil
}

func (c *Client) execRelease(ctx context.Context, initiative, slug string, op contract.Op, gate func(current *contract.Release) error) (contract.Release, error) {
	for attempt := 0; ; attempt++ {
		current, h, err := c.loadRelease(ctx, initiative, slug)
		if err != nil {
			return contract.Release{}, refusal(err)
		}
		if err := contract.CheckReleaseOp(current, op); err != nil {
			return contract.Release{}, refusal(err)
		}
		if gate != nil {
			if err := gate(current); err != nil {
				return contract.Release{}, refusal(err)
			}
		}
		next, err := contract.ApplyRelease(current, op, h.last+1)
		if err != nil {
			return contract.Release{}, refusal(err)
		}
		next.Initiative = initiative
		opType, err := contract.VocabOperation(op)
		if err != nil {
			return contract.Release{}, err
		}
		w := write{path: contract.ReleasePath(initiative, slug), opType: opType, opID: op.ID, expect: h.last}
		if op.Op == contract.OpRegistered {
			w.create, w.data = true, contract.ReleaseState(next)
		} else {
			w.data = contract.Diff(contract.ReleaseState(current), contract.ReleaseState(next))
		}
		err = c.send(ctx, w)
		switch {
		case w.create && isExists(err):
			return contract.Release{}, &client.APIError{Code: "already-registered", Message: fmt.Sprintf("release %q of initiative %s is already registered", slug, initiative)}
		case moved(err) && attempt < maxRetries:
			continue
		case err != nil:
			return contract.Release{}, refusal(err)
		}
		return *next, nil
	}
}

// RegisterRelease registers a release of a live initiative.
func (c *Client) RegisterRelease(ctx context.Context, r client.RegisterReleaseRequest) (contract.Release, error) {
	if !contract.ValidInitiativeSlug(r.Initiative) {
		return contract.Release{}, &client.APIError{Code: "invalid-initiative", Message: fmt.Sprintf("%q is not a well-formed initiative slug", r.Initiative)}
	}
	if err := c.checkInitiativeLive(ctx, r.Initiative); err != nil {
		return contract.Release{}, refusal(err)
	}
	op, err := contract.NewOp(contract.OpRegistered, r.Initiative+"."+r.Slug, c.Author(), contract.RegisteredPayload{Name: r.Name, Description: r.Description})
	if err != nil {
		return contract.Release{}, err
	}
	return c.execRelease(ctx, r.Initiative, r.Slug, op, nil)
}

// ShipRelease ships a release: the straggler gate first — refused while
// any non-terminal item targets it — then the guarded write.
func (c *Client) ShipRelease(ctx context.Context, r client.ShipReleaseRequest) (contract.Release, error) {
	op, err := contract.NewOp(contract.OpShipped, r.Initiative+"."+r.Slug, c.Author(), contract.ShippedPayload{Refs: r.Refs, Note: r.Note})
	if err != nil {
		return contract.Release{}, err
	}
	return c.execRelease(ctx, r.Initiative, r.Slug, op, func(*contract.Release) error {
		return c.checkNoOpenTargets(ctx, r.Initiative, r.Slug)
	})
}

// RetireRelease retires a release.
func (c *Client) RetireRelease(ctx context.Context, r client.RetireReleaseRequest) (contract.Release, error) {
	op, err := contract.NewOp(contract.OpRetired, r.Initiative+"."+r.Slug, c.Author(), contract.RetiredPayload{Reason: r.Reason})
	if err != nil {
		return contract.Release{}, err
	}
	return c.execRelease(ctx, r.Initiative, r.Slug, op, nil)
}

// ListReleases lists one initiative's unretired releases, shipped ones
// included.
func (c *Client) ListReleases(ctx context.Context, r client.ListReleasesRequest) ([]contract.Release, error) {
	if !contract.ValidInitiativeSlug(r.Initiative) {
		return nil, &client.APIError{Code: "invalid-initiative", Message: fmt.Sprintf("%q is not a well-formed initiative slug", r.Initiative)}
	}
	var releases []contract.Release
	for info, err := range c.cc.ListInstances(ctx, c.store, chron.Under(contract.InitiativePath(r.Initiative))) {
		if err != nil {
			return nil, refusal(err)
		}
		if !strings.Contains(info.Path, "/"+contract.ReleasesChild+"/") {
			continue
		}
		rel, err := contract.ReleaseFromState(r.Initiative, info.State)
		if err != nil {
			return nil, err
		}
		if rel.Retired {
			continue
		}
		rel.Seq = info.Seq
		releases = append(releases, *rel)
	}
	sort.Slice(releases, func(a, b int) bool { return releases[a].Slug < releases[b].Slug })
	return releases, nil
}

// checkNoOpenTargets is the straggler gate (decision 0017): shipping is
// refused while any non-terminal item of the initiative targets the
// release, the offenders named. A listing, deliberately: shipping is
// rare.
func (c *Client) checkNoOpenTargets(ctx context.Context, initiative, slug string) error {
	items, err := c.ListItems(ctx)
	if err != nil {
		return err
	}
	var open []string
	for _, it := range items {
		if it.Status.Terminal() {
			continue
		}
		if it.Initiative == initiative && it.Target == slug {
			open = append(open, it.ID)
		}
	}
	if len(open) > 0 {
		return inv("open-targets", "release %s of initiative %s is targeted by open items %s; re-target or clear them — the cut is the triage pass", slug, initiative, strings.Join(open, ", "))
	}
	return nil
}
