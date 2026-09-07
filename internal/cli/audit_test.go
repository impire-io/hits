package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/impire-io/hits/internal/cli"
)

// gitRepo is one throwaway clone: a real git repository with a main
// branch and, optionally, a fake GitHub origin identity. No mocked git,
// per the audit spec.
type gitRepo struct {
	t    *testing.T
	path string
}

func newGitRepo(t *testing.T, identity string) *gitRepo {
	t.Helper()
	r := &gitRepo{t: t, path: t.TempDir()}
	r.git("init", "-b", "main")
	r.commit("initial layout")
	if identity != "" {
		r.git("remote", "add", "origin", "git@github.com:"+identity+".git")
	}
	return r
}

// git runs one git command in the repo under a fully isolated
// configuration: no global or system config, no signing, a fixed
// identity.
func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.path}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit adds one empty commit and returns its sha.
func (r *gitRepo) commit(msg string) string {
	r.git("commit", "--allow-empty", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// mergeWork merges a work branch the GitHub way: a no-ff merge commit
// whose subject names the PR number and the branch.
func (r *gitRepo) mergeWork(prNum int, branch string) {
	r.git("checkout", "-b", branch)
	r.commit("work on " + branch)
	r.git("checkout", "main")
	r.git("merge", "--no-ff", branch, "-m",
		fmt.Sprintf("Merge pull request #%d from impire-io/%s", prNum, branch))
}

// auditOut executes one audit invocation, returning its stdout and error —
// a failing audit prints findings and fails, so both matter.
func auditOut(t *testing.T, connect cli.Connector, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := cli.Run(context.Background(), args, &out, &errOut, connect)
	return out.String(), err
}

func TestAuditClean(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")
	repo.mergeWork(9, "1")
	sha := repo.commit("a direct fix on main")

	run(t, connect, "project", "register", "hits", "HITS repo")
	id := itemID(t, run(t, connect, "create", "--type", "bug", "the CLI panics"))
	run(t, connect, "resolve", id, "--fixed-by", "pr:impire-io/hits#9 merged")
	id = itemID(t, run(t, connect, "create", "--type", "task", "--project", "hits", "sync the docs"))
	run(t, connect, "resolve", id, "--fixed-by", "commit:"+sha+" done")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err != nil {
		t.Fatalf("clean audit failed: %v\n%s", err, out)
	}
	wantContains(t, out, "audited 2 items across 1 repos: 0 failed, 0 unverifiable")
}

func TestAuditSquashMergeVerifiesPR(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")
	repo.commit("polish the table (#12)")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "table misaligned"))
	run(t, connect, "resolve", id, "--fixed-by", "pr:impire-io/hits#12 squashed")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err != nil {
		t.Fatalf("squash-merged pr should verify: %v\n%s", err, out)
	}
	wantContains(t, out, "0 failed, 0 unverifiable")
}

func TestAuditPrUnmerged(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "the CLI panics"))
	run(t, connect, "resolve", id, "--fixed-by", "pr:impire-io/hits#99 claimed merged")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err == nil || !strings.Contains(err.Error(), "1 failure(s)") {
		t.Fatalf("want 1 failure, got err %v\n%s", err, out)
	}
	wantContains(t, out, "pr-unmerged", "pr:impire-io/hits#99", "no commit on")
}

func TestAuditCommitUnmerged(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")
	repo.git("checkout", "-b", "stray")
	sha := repo.commit("never merged")
	repo.git("checkout", "main")

	run(t, connect, "project", "register", "hits", "HITS repo")
	id := itemID(t, run(t, connect, "create", "--type", "task", "--project", "hits", "sync the docs"))
	run(t, connect, "resolve", id, "--fixed-by", "commit:"+sha+" claimed done")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err == nil || !strings.Contains(err.Error(), "1 failure(s)") {
		t.Fatalf("want 1 failure, got err %v\n%s", err, out)
	}
	wantContains(t, out, "commit-unmerged", "commit:"+sha, "not an ancestor of main in hits")
}

func TestAuditQualifiedCommitRef(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")
	merged := repo.commit("landed on main")
	repo.git("checkout", "-b", "stray")
	stray := repo.commit("never merged")
	repo.git("checkout", "main")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "fixed by direct commit"))
	run(t, connect, "resolve", id, "--fixed-by", "commit:impire-io/hits@"+merged+" done")
	id = itemID(t, run(t, connect, "create", "--type", "bug", "claimed fixed"))
	run(t, connect, "resolve", id, "--fixed-by", "commit:impire-io/hits@"+stray+" claimed")
	id = itemID(t, run(t, connect, "create", "--type", "bug", "fixed elsewhere"))
	run(t, connect, "resolve", id, "--fixed-by", "commit:impire-io/elsewhere@"+merged+" there")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err == nil || !strings.Contains(err.Error(), "1 failure(s)") {
		t.Fatalf("want 1 failure, got err %v\n%s", err, out)
	}
	wantContains(t, out,
		"commit-unmerged", "commit:impire-io/hits@"+stray, "not an ancestor of main in impire-io/hits",
		"unverifiable", "no --repo clone has origin impire-io/elsewhere",
		"1 failed, 1 unverifiable")
}

func TestAuditMergedOpenAndUntracked(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")
	repo.mergeWork(5, "1")
	repo.mergeWork(6, "42")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "still being worked"))
	if id != "1" {
		t.Fatalf("first item minted %q, want 1", id)
	}

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err == nil || !strings.Contains(err.Error(), "2 failure(s)") {
		t.Fatalf("want 2 failures, got err %v\n%s", err, out)
	}
	wantContains(t, out,
		"merged-open", "item 1 is open",
		"merged-untracked", "no live tracker item 42")
}

func TestAuditTombstonedSkippedButNotLive(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")
	repo.mergeWork(7, "1")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "filed by mistake"))
	run(t, connect, "tombstone", id, "duplicate filing")
	run(t, connect, "create", "--type", "bug", "the walk must reach me")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err == nil || !strings.Contains(err.Error(), "1 failure(s)") {
		t.Fatalf("want 1 failure, got err %v\n%s", err, out)
	}
	wantContains(t, out, "merged-untracked", "no live tracker item 1", "audited 2 items")
}

func TestAuditUnverifiableWarnsOnly(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")
	sha := repo.commit("somewhere")

	run(t, connect, "project", "register", "other", "an unmapped repo")
	id := itemID(t, run(t, connect, "create", "--type", "bug", "cross-repo symptom"))
	run(t, connect, "resolve", id, "--fixed-by", "pr:impire-io/elsewhere#3 merged there")
	id = itemID(t, run(t, connect, "create", "--type", "bug", "bare pr ref"))
	run(t, connect, "resolve", id, "--fixed-by", "pr:#4 unqualified")
	id = itemID(t, run(t, connect, "create", "--type", "task", "--project", "other", "unmapped slug"))
	run(t, connect, "resolve", id, "--fixed-by", "commit:"+sha+" done")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err != nil {
		t.Fatalf("warnings alone must not fail: %v\n%s", err, out)
	}
	wantContains(t, out,
		"unverifiable", "no --repo clone has origin impire-io/elsewhere",
		"not the owner/repo#N form",
		"no --repo mapping for located-in other",
		"3 unverifiable")
}

func TestAuditActionRefsProduceNothing(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")

	id := itemID(t, run(t, connect, "create", "--type", "improvement", "cut the release"))
	run(t, connect, "resolve", id, "--fixed-by", "action:release hits 0.5.0 tag v0.5.0 observed")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path)
	if err != nil {
		t.Fatalf("action-only refs must audit clean: %v\n%s", err, out)
	}
	wantContains(t, out, "0 failed, 0 unverifiable")
}

func TestAuditJSON(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")

	id := itemID(t, run(t, connect, "create", "--type", "bug", "the CLI panics"))
	run(t, connect, "resolve", id, "--fixed-by", "pr:impire-io/hits#99 claimed merged")

	out, err := auditOut(t, connect, "--json", "audit", "--repo", "hits="+repo.path)
	if err == nil {
		t.Fatalf("want failure exit, got clean run\n%s", out)
	}
	var reply struct {
		Findings []struct {
			Level, Kind, Item, Ref string
		} `json:"findings"`
		Items, Repos, Failures, Warnings int
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode --json output: %v\n%s", err, out)
	}
	if len(reply.Findings) != 1 || reply.Findings[0].Kind != "pr-unmerged" ||
		reply.Findings[0].Level != "fail" || reply.Findings[0].Item != "1" {
		t.Errorf("findings = %+v", reply.Findings)
	}
	if reply.Items != 1 || reply.Repos != 1 || reply.Failures != 1 || reply.Warnings != 0 {
		t.Errorf("counts = %+v", reply)
	}
}

func TestAuditRejectsBeforeDialing(t *testing.T) {
	err := runErr(t, guardConnector(t), "audit")
	if !strings.Contains(err.Error(), "at least one --repo") {
		t.Errorf("no mapping: %v", err)
	}
	err = runErr(t, guardConnector(t), "audit", "--repo", "hits")
	if !strings.Contains(err.Error(), "bad --repo") {
		t.Errorf("malformed mapping: %v", err)
	}
	err = runErr(t, guardConnector(t), "audit", "--repo", "hits="+t.TempDir())
	if !strings.Contains(err.Error(), "no origin/main or main") {
		t.Errorf("not a repo: %v", err)
	}
	err = runErr(t, guardConnector(t), "audit", "--repo", "hits="+t.TempDir(), "--fan", "0")
	if !strings.Contains(err.Error(), "--fan 0: want at least 1") {
		t.Errorf("zero fan: %v", err)
	}
}

func TestAuditFanIsConfigurable(t *testing.T) {
	h := startStore(t)
	connect := h.connector()
	t.Setenv("HITS_ACTOR", "daan")
	repo := newGitRepo(t, "impire-io/hits")

	run(t, connect, "create", "--type", "bug", "one symptom")
	run(t, connect, "create", "--type", "bug", "another symptom")

	out, err := auditOut(t, connect, "audit", "--repo", "hits="+repo.path, "--fan", "1")
	if err != nil {
		t.Fatalf("--fan 1 walk failed: %v\n%s", err, out)
	}
	wantContains(t, out, "audited 2 items")
}
