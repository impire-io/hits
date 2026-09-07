package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/impire-io/hits/client"
	"github.com/impire-io/hits/contract"
)

// runAudit holds the tracker against git: a resolved item's fixed-by refs
// must be true on the named repos' main, and merged work must not leave
// its item open. Contradictions fail the command; refs the mapping cannot
// check only warn.
func runAudit(inv *invocation) error {
	fs := inv.flagSet("audit", "audit --repo <slug>=<path> [--repo ...] [--fan <n>]")
	var mappings multiFlag
	fs.Var(&mappings, "repo", "project slug and its local clone, <slug>=<path> (repeatable)")
	fan := fs.Int("fan", defaultFan, "concurrent item gets during the corpus walk")
	if err := fs.Parse(inv.args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	if len(mappings) == 0 {
		fs.Usage()
		return errors.New("audit: at least one --repo <slug>=<path> mapping is required")
	}
	if *fan < 1 {
		return fmt.Errorf("audit: --fan %d: want at least 1", *fan)
	}
	slugs := make([]string, len(mappings))
	paths := make([]string, len(mappings))
	for i, m := range mappings {
		slug, path, ok := strings.Cut(m, "=")
		if !ok || slug == "" || path == "" {
			return fmt.Errorf("audit: bad --repo %q: want <slug>=<path>", m)
		}
		slugs[i], paths[i] = slug, path
	}
	evs := make([]*repoEvidence, len(mappings))
	loadErrs := make([]error, len(mappings))
	var wg sync.WaitGroup
	for i := range mappings {
		wg.Add(1)
		go func() {
			defer wg.Done()
			evs[i], loadErrs[i] = loadRepo(paths[i])
		}()
	}
	wg.Wait()
	repos := make(map[string]*repoEvidence, len(mappings))
	for i, m := range mappings {
		if loadErrs[i] != nil {
			return fmt.Errorf("audit: --repo %s: %w", m, loadErrs[i])
		}
		repos[slugs[i]] = evs[i]
	}

	c, closeConn, err := inv.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	items, err := walkItems(inv.ctx, c, *fan)
	if err != nil {
		return err
	}

	findings := auditRefs(items, repos)
	findings = append(findings, auditMerges(items, repos)...)
	return inv.printAudit(findings, len(items), len(repos))
}

// repoEvidence is one clone's git truth, read once: the ref audited
// against, every commit subject on it, and the clone's GitHub identity
// (empty without an origin remote).
type repoEvidence struct {
	path     string
	mainRef  string
	identity string
	subjects []string
}

// loadRepo reads one clone's evidence. The audit ref is origin/main where
// it exists, else main — fetching stays the operator's job, and the
// network is never touched.
func loadRepo(path string) (*repoEvidence, error) {
	ev := &repoEvidence{path: path}
	for _, ref := range []string{"origin/main", "main"} {
		if _, err := git(path, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			ev.mainRef = ref
			break
		}
	}
	if ev.mainRef == "" {
		return nil, errors.New("no origin/main or main to audit against")
	}
	log, err := git(path, "log", "--format=%s", ev.mainRef)
	if err != nil {
		return nil, err
	}
	ev.subjects = strings.Split(log, "\n")
	if url, err := git(path, "remote", "get-url", "origin"); err == nil {
		ev.identity = repoIdentity(url)
	}
	return ev, nil
}

// git runs one git command against a clone and returns its trimmed stdout.
func git(path string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", path}, args...)...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// repoIdentity extracts owner/repo from an origin URL in its https,
// ssh://, or scp-like forms; empty when the shape is unrecognizable.
func repoIdentity(url string) string {
	s := strings.TrimSuffix(strings.TrimSpace(url), ".git")
	parts := strings.Split(strings.ReplaceAll(s, ":", "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	owner, repo := parts[len(parts)-2], parts[len(parts)-1]
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

// walkItems reads the whole corpus: IDs are server-minted dense integers,
// so the walk from 1 to the first not-found is complete by construction,
// and no index is consulted. Gets fan out a window of fan at a time so
// the wire round-trips overlap; density means everything past the first
// gap is noise, so errors beyond it are discarded with it.
func walkItems(ctx context.Context, c *client.Client, fan int) ([]contract.Item, error) {
	var items []contract.Item
	for base := 1; ; base += fan {
		batch := make([]*contract.Item, fan)
		errs := make([]error, fan)
		var wg sync.WaitGroup
		for i := range fan {
			wg.Add(1)
			go func() {
				defer wg.Done()
				it, err := c.GetItem(ctx, strconv.Itoa(base+i))
				var apiErr *client.APIError
				switch {
				case err == nil:
					batch[i] = &it
				case errors.As(err, &apiErr) && apiErr.Code == "not-found":
					// the end of the corpus falls in this window
				default:
					errs[i] = fmt.Errorf("get %d: %w", base+i, err)
				}
			}()
		}
		wg.Wait()
		for i, it := range batch {
			if it == nil {
				if errs[i] != nil {
					return nil, errs[i]
				}
				return items, nil
			}
			items = append(items, *it)
		}
	}
}

// finding is one audit result. Kind is a stable vocabulary: pr-unmerged,
// commit-unmerged, merged-open, and merged-untracked fail the audit;
// unverifiable only warns.
type finding struct {
	Level  string `json:"level"`
	Kind   string `json:"kind"`
	Item   string `json:"item,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Detail string `json:"detail"`
}

// auditRefs verifies resolved items' pr: and commit: refs against the
// mapped clones. action: refs carry their evidence in the note and are no
// business of git's.
func auditRefs(items []contract.Item, repos map[string]*repoEvidence) []finding {
	findings := []finding{}
	for _, it := range items {
		if it.Tombstoned || it.Status != contract.Resolved {
			continue
		}
		for _, ref := range it.FixedBy {
			if ref.PR != "" {
				findings = append(findings, auditPR(it, ref.PR, repos)...)
			}
			if ref.Commit != "" {
				findings = append(findings, auditCommit(it, ref.Commit, repos)...)
			}
		}
	}
	return findings
}

// auditPR checks one owner/repo#N ref: some commit subject on that
// repo's main must reference the pull request, in the merge or squash
// form (playbook 03's verifiability rule).
func auditPR(it contract.Item, ref string, repos map[string]*repoEvidence) []finding {
	identity, num, ok := strings.Cut(ref, "#")
	if !ok || !strings.Contains(identity, "/") || num == "" {
		return []finding{{Level: "warn", Kind: "unverifiable", Item: it.ID, Ref: "pr:" + ref,
			Detail: "not the owner/repo#N form, so no clone can vouch for it"}}
	}
	for _, ev := range repos {
		if ev.identity != identity {
			continue
		}
		if prMerged(ev.subjects, num) {
			return nil
		}
		return []finding{{Level: "fail", Kind: "pr-unmerged", Item: it.ID, Repo: identity, Ref: "pr:" + ref,
			Detail: fmt.Sprintf("no commit on %s references #%s", ev.mainRef, num)}}
	}
	return []finding{{Level: "warn", Kind: "unverifiable", Item: it.ID, Repo: identity, Ref: "pr:" + ref,
		Detail: "no --repo clone has origin " + identity}}
}

// prMerged reports whether a subject references pull request num: the
// GitHub merge form ("Merge pull request #N from …") or the squash form
// (a "(#N)" suffix). Exact string shapes — #9 never matches #90.
func prMerged(subjects []string, num string) bool {
	merge, squash := "Merge pull request #"+num+" from ", "(#"+num+")"
	for _, s := range subjects {
		if strings.HasPrefix(s, merge) || strings.HasSuffix(s, squash) {
			return true
		}
	}
	return false
}

// auditCommit checks one commit ref as an ancestor of main. A qualified
// ref (owner/repo@sha, the form the corpus carries) names its repo the
// way a pr ref does; a bare sha is checked in every mapped clone of the
// item's located-in slugs.
func auditCommit(it contract.Item, ref string, repos map[string]*repoEvidence) []finding {
	if identity, sha, ok := strings.Cut(ref, "@"); ok && strings.Contains(identity, "/") && sha != "" {
		for _, ev := range repos {
			if ev.identity != identity {
				continue
			}
			if isAncestor(ev.path, sha, ev.mainRef) {
				return nil
			}
			return []finding{{Level: "fail", Kind: "commit-unmerged", Item: it.ID, Repo: identity,
				Ref: "commit:" + ref, Detail: "not an ancestor of main in " + identity}}
		}
		return []finding{{Level: "warn", Kind: "unverifiable", Item: it.ID, Repo: identity,
			Ref: "commit:" + ref, Detail: "no --repo clone has origin " + identity}}
	}

	sha := ref
	var checked []string
	for _, slug := range it.LocatedIn {
		ev, ok := repos[slug]
		if !ok {
			continue
		}
		if isAncestor(ev.path, sha, ev.mainRef) {
			return nil
		}
		checked = append(checked, slug)
	}
	if len(checked) == 0 {
		detail := "the item names no located-in project"
		if len(it.LocatedIn) > 0 {
			detail = "no --repo mapping for located-in " + strings.Join(it.LocatedIn, ", ")
		}
		return []finding{{Level: "warn", Kind: "unverifiable", Item: it.ID, Ref: "commit:" + sha, Detail: detail}}
	}
	return []finding{{Level: "fail", Kind: "commit-unmerged", Item: it.ID,
		Repo: strings.Join(checked, ", "), Ref: "commit:" + sha,
		Detail: "not an ancestor of main in " + strings.Join(checked, ", ")}}
}

// isAncestor reports whether sha sits on mainRef. merge-base
// --is-ancestor exits 0 for yes, 1 for no, and anything else — an
// unknown sha included — is equally not-on-main.
func isAncestor(path, sha, mainRef string) bool {
	return exec.Command("git", "-C", path, "merge-base", "--is-ancestor", sha, mainRef).Run() == nil
}

// mergeSubject is a GitHub merge commit from a bare-integer branch — the
// post-cutover naming rule makes that integer a work ID. Squash merges
// carry no branch name, so they cannot appear here.
var mergeSubject = regexp.MustCompile(`^Merge pull request #\d+ from [^/ ]+/(\d+)$`)

// auditMerges flags merged work IDs whose item is missing, tombstoned, or
// not terminal: merged work must not leave its record open.
func auditMerges(items []contract.Item, repos map[string]*repoEvidence) []finding {
	byID := make(map[string]contract.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	slugs := make([]string, 0, len(repos))
	for slug := range repos {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	findings := []finding{}
	for _, slug := range slugs {
		for _, s := range repos[slug].subjects {
			m := mergeSubject.FindStringSubmatch(s)
			if m == nil {
				continue
			}
			id := m[1]
			it, ok := byID[id]
			switch {
			case !ok || it.Tombstoned:
				findings = append(findings, finding{Level: "fail", Kind: "merged-untracked",
					Item: id, Repo: slug, Ref: s,
					Detail: "merged work names no live tracker item " + id})
			case !it.Status.Terminal():
				findings = append(findings, finding{Level: "fail", Kind: "merged-open",
					Item: id, Repo: slug, Ref: s,
					Detail: fmt.Sprintf("item %s is %s, and merged work must not leave its item open", id, it.Status)})
			}
		}
	}
	return findings
}

// printAudit renders the findings and the summary; any failure makes the
// command's exit non-zero, warnings alone do not.
func (inv *invocation) printAudit(findings []finding, items, repos int) error {
	fails, warns := 0, 0
	for _, f := range findings {
		if f.Level == "fail" {
			fails++
		} else {
			warns++
		}
	}
	if inv.json {
		if err := emit(inv.out, struct {
			Findings []finding `json:"findings"`
			Items    int       `json:"items"`
			Repos    int       `json:"repos"`
			Failures int       `json:"failures"`
			Warnings int       `json:"warnings"`
		}{findings, items, repos, fails, warns}); err != nil {
			return err
		}
	} else {
		for _, f := range findings {
			scope := ""
			if f.Item != "" {
				scope = " item " + f.Item
			}
			if f.Repo != "" {
				scope += " [" + f.Repo + "]"
			}
			fmt.Fprintf(inv.out, "%s %s%s %s: %s\n", strings.ToUpper(f.Level), f.Kind, scope, f.Ref, f.Detail)
		}
		fmt.Fprintf(inv.out, "audited %d items across %d repos: %d failed, %d unverifiable\n", items, repos, fails, warns)
	}
	if fails > 0 {
		return fmt.Errorf("audit: %d failure(s)", fails)
	}
	return nil
}
