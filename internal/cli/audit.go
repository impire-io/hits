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
	"github.com/impire-io/hits/client/chronicle"
	"github.com/impire-io/hits/contract"
)

// runAudit holds the tracker against git: a resolved item's fixed-by refs
// must be true on the named repos' main, and merged work must not leave
// its item open. Contradictions fail the command; refs the mapping cannot
// check only warn.
func runAudit(inv *invocation) error {
	fs := inv.flagSet("audit", "audit [--repo <slug>=<path> ...] [--since-seq <n>] [--initiative <i>] [--fan <n>]")
	var mappings multiFlag
	fs.Var(&mappings, "repo", "project slug and its local clone, <slug>=<path> (repeatable)")
	initiative := fs.String("initiative", "", "narrow the tracker side to one initiative")
	fan := fs.Int("fan", defaultFan, "concurrent history reads when --since-seq is given")
	sinceSeq := fs.Uint64("since-seq", 0, "report every write after this store sequence whose author chronicle did not vouch for (the cutover's sequence)")
	if err := fs.Parse(inv.args); err != nil {
		return err
	}
	if err := noTrailing(fs); err != nil {
		return err
	}
	// The cutover's read stands on its own; the git evidence needs a clone.
	if len(mappings) == 0 && *sinceSeq == 0 {
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
	items, err := c.ListAllItems(inv.ctx)
	if err != nil {
		return err
	}
	releases, err := loadReleases(inv.ctx, c)
	if err != nil {
		return err
	}

	findings := []finding{}
	if len(repos) > 0 {
		findings = append(findings, auditRefs(items, repos, *initiative)...)
		findings = append(findings, auditMerges(items, repos, *initiative)...)
	}
	findings = append(findings, auditTargets(items, releases, *initiative)...)
	findings = append(findings, auditGaps(items, *initiative)...)
	if *sinceSeq > 0 {
		cc, ok := c.(*chronicle.Client)
		if !ok {
			return fmt.Errorf("--since-seq: %w", errNotChronicle)
		}
		more, err := auditUnverified(inv.ctx, cc, items, *sinceSeq, *fan)
		if err != nil {
			return err
		}
		findings = append(findings, more...)
	}
	return inv.printAudit(findings, len(items), len(repos))
}

// loadReleases reads every live initiative's release vocabulary, keyed by
// <initiative>.<slug> — the net the target audit checks against. Retired
// releases are not listed, so a stray target on one reads as unknown,
// which is exactly the stray guardless retirement leaves (decision 0017).
func loadReleases(ctx context.Context, c client.Client) (map[string]contract.Release, error) {
	initiatives, err := c.ListInitiatives(ctx)
	if err != nil {
		return nil, fmt.Errorf("list initiatives: %w", err)
	}
	releases := map[string]contract.Release{}
	for _, i := range initiatives {
		rs, err := c.ListReleases(ctx, client.ListReleasesRequest{Initiative: i.Slug})
		if err != nil {
			return nil, fmt.Errorf("list releases of %s: %w", i.Slug, err)
		}
		for _, r := range rs {
			releases[r.Initiative+"."+r.Slug] = r
		}
	}
	return releases, nil
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
// mapped clones; a non-empty initiative narrows to that initiative's
// items. action: refs carry their evidence in the note and are no
// business of git's.
func auditRefs(items []contract.Item, repos map[string]*repoEvidence, initiative string) []finding {
	findings := []finding{}
	for _, it := range items {
		if it.Tombstoned || it.Status != contract.Resolved {
			continue
		}
		if initiative != "" && it.Initiative != initiative {
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

// mergeSubject is a GitHub merge commit and its branch tail; a tail that
// parses as an item ID — bare-integer (the post-cutover naming rule) or
// initiative-prefixed (decision 0016) — names a work ID. Squash merges
// carry no branch name, so they cannot appear here.
var mergeSubject = regexp.MustCompile(`^Merge pull request #\d+ from [^/ ]+/(.+)$`)

// auditMerges flags merged work IDs whose item is missing, tombstoned, or
// not terminal: merged work must not leave its record open. A non-empty
// initiative narrows to work IDs of that initiative.
func auditMerges(items []contract.Item, repos map[string]*repoEvidence, initiative string) []finding {
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
			idInitiative, ok := contract.ParseItemID(id)
			if !ok {
				continue // a named branch, not a work ID
			}
			if initiative != "" && idInitiative != initiative {
				continue
			}
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

// auditTargets nets the release invariants over the walked corpus
// (decision 0017): a non-terminal item's target must name a live,
// unshipped release of its own initiative. Write-time checks enforce
// this; the audit catches what slips past them — replay drift, and the
// strays guardless retirement deliberately leaves behind.
func auditTargets(items []contract.Item, releases map[string]contract.Release, initiative string) []finding {
	findings := []finding{}
	for _, it := range items {
		if it.Tombstoned || it.Status.Terminal() || it.Target == "" {
			continue
		}
		if initiative != "" && it.Initiative != initiative {
			continue
		}
		r, ok := releases[it.Initiative+"."+it.Target]
		switch {
		case !ok:
			findings = append(findings, finding{Level: "fail", Kind: "target-unknown",
				Item: it.ID, Ref: "target:" + it.Target,
				Detail: fmt.Sprintf("targets %q, which is not in initiative %s's release vocabulary (unregistered or retired) — re-target or clear it", it.Target, it.Initiative)})
		case r.Shipped:
			findings = append(findings, finding{Level: "fail", Kind: "target-terminal",
				Item: it.ID, Ref: "target:" + it.Target,
				Detail: fmt.Sprintf("open item targets shipped release %s of %s — the straggler gate should have made this impossible", it.Target, it.Initiative)})
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

// auditGaps lists the numbers an initiative's counter spent without an
// item landing — a crash between the mint and the create (hits-hq
// 02-DESIGN/the-store.md § the counter). A warning: a gap is never a reuse.
// Tombstoned items count as landed: the number was not spent in vain.
func auditGaps(items []contract.Item, initiative string) []finding {
	highest := map[string]int{}
	seen := map[string]map[int]bool{}
	for _, it := range items {
		prefix, ok := contract.ParseItemID(it.ID)
		if !ok || prefix == "" {
			continue
		}
		if initiative != "" && prefix != initiative {
			continue
		}
		n, err := strconv.Atoi(it.ID[len(prefix)+1:])
		if err != nil {
			continue
		}
		if seen[prefix] == nil {
			seen[prefix] = map[int]bool{}
		}
		seen[prefix][n] = true
		if n > highest[prefix] {
			highest[prefix] = n
		}
	}
	findings := []finding{}
	prefixes := make([]string, 0, len(highest))
	for p := range highest {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, p := range prefixes {
		for n := 1; n <= highest[p]; n++ {
			if !seen[p][n] {
				findings = append(findings, finding{Level: "warn", Kind: "numbering-gap", Item: fmt.Sprintf("%s-%d", p, n),
					Detail: "the counter spent this number and no item landed on it: a crash between the mint and the create"})
			}
		}
	}
	return findings
}

// auditUnverified reads every item's history after the given sequence
// and reports the writes chronicle did not vouch for — after the cutover,
// every live write goes through the node, so an unverified one is a bare
// writer's (hits-hq decision 0019).
func auditUnverified(ctx context.Context, c *chronicle.Client, items []contract.Item, since uint64, fan int) ([]finding, error) {
	results := make([][]finding, len(items))
	errs := make([]error, len(items))
	sem := make(chan struct{}, fan)
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			for op, err := range c.Chronicle().History(ctx, c.Store(), contract.ItemPath(it.ID)) {
				if err != nil {
					errs[i] = fmt.Errorf("history of %s: %w", it.ID, err)
					return
				}
				if op.Seq > since && !op.Verified {
					results[i] = append(results[i], finding{Level: "fail", Kind: "unverified-write", Item: it.ID,
						Detail: fmt.Sprintf("operation %s at sequence %d names author %s and chronicle did not vouch for it", op.Type, op.Seq, op.Author)})
				}
			}
		}()
	}
	wg.Wait()
	findings := []finding{}
	for i := range items {
		if errs[i] != nil {
			return nil, errs[i]
		}
		findings = append(findings, results[i]...)
	}
	return findings, nil
}
