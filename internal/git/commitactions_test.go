package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func actionHash(t *testing.T, r *Repo, ref string) string {
	t.Helper()
	h, err := r.panelCommit(ref)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCommitPatchRootMergeAndBinary(t *testing.T) {
	r, git := repo(t)
	patch, err := r.CommitPatch("HEAD")
	if err != nil || !strings.Contains(patch, "new file mode") || !strings.Contains(patch, "+bee") {
		t.Fatalf("root patch %q, %v", patch, err)
	}
	git("switch", "-qc", "side")
	write(t, r.Root, "binary", "\x00\x01\x02\x03")
	git("add", ".")
	git("commit", "-qm", "binary")
	git("switch", "-q", "main")
	git("commit", "--allow-empty", "-qm", "main change")
	git("merge", "--no-ff", "-qm", "merge", "side")
	patch, err = r.CommitPatch("HEAD")
	if err != nil || !strings.Contains(patch, "GIT binary patch") {
		t.Fatalf("merge patch %q, %v", patch, err)
	}
	if _, err := r.run([]byte(patch), "apply", "--reverse", "--check", "--binary", "-"); err != nil {
		t.Fatalf("patch cannot be reversed: %v", err)
	}
}

func TestComparePatchDirectionAndWorkingTree(t *testing.T) {
	r, git := repo(t)
	first := actionHash(t, r, "HEAD")
	write(t, r.Root, "b.txt", "new\n")
	git("add", ".")
	git("commit", "-qm", "second")
	patch, err := r.ComparePatch(first, "HEAD")
	if err != nil || !strings.Contains(patch, "-bee\n+new") {
		t.Fatalf("forward patch %q, %v", patch, err)
	}
	patch, err = r.ComparePatch("HEAD", first)
	if err != nil || !strings.Contains(patch, "-new\n+bee") {
		t.Fatalf("reverse patch %q, %v", patch, err)
	}
	write(t, r.Root, "b.txt", "staged\n")
	git("add", ".")
	write(t, r.Root, "a.txt", "unstaged\n")
	write(t, r.Root, "untracked", "private\n")
	patch, err = r.ComparePatch("HEAD", "")
	if err != nil || !strings.Contains(patch, "+staged") || !strings.Contains(patch, "+unstaged") || strings.Contains(patch, "private") {
		t.Fatalf("working patch %q, %v", patch, err)
	}
	for _, invalid := range []string{"", "--output=/tmp/bad", "HEAD\nHEAD", "missing"} {
		if _, err := r.CommitPatch(invalid); err == nil {
			t.Errorf("accepted revision %q", invalid)
		}
		if _, err := r.ComparePatch("HEAD", invalid); invalid != "" && err == nil {
			t.Errorf("accepted comparison revision %q", invalid)
		}
	}
}

func TestRemoteWebURL(t *testing.T) {
	hash := strings.Repeat("a", 40)
	for _, tc := range []struct {
		raw, suffix string
		tree        bool
	}{
		{"https://user:secret@github.com/org/repo.git?token=secret#fragment", "/org/repo/commit/", false},
		{"git@github.com:org/repo.git", "/org/repo/tree/", true},
		{"ssh://git@gitlab.com:2222/group/sub/repo.git", "/group/sub/repo/-/commit/", false},
		{"https://gitlab.com/group/repo.git", "/group/repo/-/tree/", true},
		{"git@bitbucket.org:org/repo.git", "/org/repo/commits/", false},
		{"https://bitbucket.org/org/repo.git", "/org/repo/src/", true},
	} {
		got, err := RemoteWebURL(tc.raw, hash, tc.tree)
		if err != nil || !strings.HasSuffix(got, tc.suffix+hash) || strings.Contains(got, "secret") || !strings.HasPrefix(got, "https://") {
			t.Errorf("%q => %q, %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{"https://unknown.test/org/repo.git", "file:///tmp/repo", "https://github.com/org/../repo", "https://github.com/repo"} {
		if _, err := RemoteWebURL(raw, hash, false); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if _, err := RemoteWebURL("https://github.com/org/repo", "HEAD", false); err == nil {
		t.Fatal("accepted non-hash")
	}
}

func TestAddDetachedWorktree(t *testing.T) {
	r, _ := repo(t)
	path := filepath.Join(t.TempDir(), "checkout with spaces")
	if err := r.AddWorktree(path, "HEAD"); err != nil {
		t.Fatal(err)
	}
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := other.Status()
	if err != nil || st.Branch != "" || st.Head != actionHash(t, r, "HEAD") {
		t.Fatalf("detached checkout %+v, %v", st, err)
	}
	for _, path := range []string{"", "--force", "/ssh:another-host/tmp/checkout"} {
		if err := r.AddWorktree(path, "HEAD"); err == nil {
			t.Errorf("accepted path %q", path)
		}
	}
}

func TestRecomposeStagesRangeAndPreservesBackup(t *testing.T) {
	r, git := repo(t)
	first := actionHash(t, r, "HEAD")
	write(t, r.Root, "b.txt", "second\n")
	git("add", ".")
	git("commit", "-qm", "second message")
	selected := actionHash(t, r, "HEAD")
	write(t, r.Root, "a.txt", "third\n")
	git("add", ".")
	git("commit", "-qm", "third message")
	oldHead := actionHash(t, r, "HEAD")
	backup, message, err := r.Recompose(selected)
	if err != nil {
		t.Fatal(err)
	}
	if message != "second message\n\nthird message" {
		t.Fatalf("message %q", message)
	}
	if !strings.HasPrefix(backup, "refs/mayoi/recompose/") || actionHash(t, r, backup) != oldHead || actionHash(t, r, "ORIG_HEAD") != oldHead || actionHash(t, r, "HEAD") != first {
		t.Fatalf("incorrect reset or backup: %s", backup)
	}
	st, err := r.Status()
	if err != nil || len(st.Entries) != 2 {
		t.Fatalf("staged state %+v, %v", st, err)
	}
	for _, e := range st.Entries {
		if !e.Staged() || e.Unstaged() {
			t.Errorf("entry %+v", e)
		}
	}
	git("reset", "--hard", backup)
	if actionHash(t, r, "HEAD") != oldHead {
		t.Fatal("backup cannot restore HEAD")
	}
	st, err = r.Status()
	if err != nil || len(st.Entries) != 0 {
		t.Fatalf("restored state %+v, %v", st, err)
	}
}

func TestRecomposeRejectsWithoutChangingRefs(t *testing.T) {
	for _, condition := range []string{"root", "dirty", "untracked", "operation", "nonancestor", "merge"} {
		t.Run(condition, func(t *testing.T) {
			r, git := repo(t)
			git("commit", "--allow-empty", "-qm", "second")
			selected := "HEAD"
			switch condition {
			case "root":
				selected = "HEAD^"
			case "dirty":
				write(t, r.Root, "b.txt", "dirty\n")
			case "untracked":
				write(t, r.Root, "untracked", "new\n")
			case "operation":
				if err := os.Mkdir(filepath.Join(r.Dir, "sequencer"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "nonancestor", "merge":
				git("switch", "-qc", "side", "HEAD^")
				git("commit", "--allow-empty", "-qm", "side")
				selected = actionHash(t, r, "HEAD")
				git("switch", "-q", "main")
				if condition == "merge" {
					git("merge", "--no-ff", "-qm", "merge", "side")
				}
			}
			before := actionHash(t, r, "HEAD")
			if _, _, err := r.Recompose(selected); err == nil {
				t.Fatal("accepted unsafe range")
			}
			if actionHash(t, r, "HEAD") != before {
				t.Fatal("changed HEAD on failure")
			}
			refs, err := r.run(nil, "for-each-ref", "--format=%(refname)", "refs/mayoi/recompose/")
			if err != nil || len(refs) != 0 {
				t.Fatalf("created backup on failure: %q, %v", refs, err)
			}
		})
	}
}

// Build the transaction from the validated snapshot, then switch HEAD before
// applying it. This reproduces the external-client race deterministically.
func TestRecomposeTransactionBindsOriginalBranch(t *testing.T) {
	r, git := repo(t)
	parent := actionHash(t, r, "HEAD")
	git("commit", "--allow-empty", "-qm", "second")
	oldHead := actionHash(t, r, "HEAD")
	git("branch", "other")
	backup := "refs/mayoi/recompose/race"
	transaction := recomposeTransaction("refs/heads/main", oldHead, parent, backup)
	git("switch", "-q", "other")
	if err := r.checkRecomposeHeadTarget("refs/heads/main"); err == nil {
		t.Fatal("preflight accepted a changed symbolic HEAD target")
	}
	// A switch after the recheck must still never redirect the reset to other.
	if _, err := r.run([]byte(transaction), "update-ref", "--stdin"); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"HEAD", "other"} {
		if actionHash(t, r, ref) != oldHead {
			t.Errorf("changed unrelated %s after switched HEAD", ref)
		}
	}
	if actionHash(t, r, "main") != parent || actionHash(t, r, backup) != oldHead {
		t.Fatal("original branch was not reset with its backup")
	}
}

func TestRecomposeDetachedHEAD(t *testing.T) {
	r, git := repo(t)
	parent := actionHash(t, r, "HEAD")
	git("commit", "--allow-empty", "-qm", "second")
	oldHead := actionHash(t, r, "HEAD")
	git("switch", "--detach", "-q", oldHead)
	backup, _, err := r.Recompose("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if actionHash(t, r, "HEAD") != parent || actionHash(t, r, "main") != oldHead || actionHash(t, r, backup) != oldHead {
		t.Fatal("detached reset changed the branch or lost the backup")
	}
	st, err := r.Status()
	if err != nil || st.Branch != "" {
		t.Fatalf("HEAD must remain detached: %+v, %v", st, err)
	}
}

func TestRecomposeTransactionRejectsChangedCommit(t *testing.T) {
	r, git := repo(t)
	parent := actionHash(t, r, "HEAD")
	git("commit", "--allow-empty", "-qm", "second")
	oldHead := actionHash(t, r, "HEAD")
	backup := "refs/mayoi/recompose/stale"
	transaction := recomposeTransaction("refs/heads/main", oldHead, parent, backup)
	git("commit", "--allow-empty", "-qm", "concurrent third")
	newHead := actionHash(t, r, "HEAD")
	if _, err := r.run([]byte(transaction), "update-ref", "--stdin"); err == nil {
		t.Fatal("transaction accepted a changed HEAD commit")
	}
	if actionHash(t, r, "HEAD") != newHead {
		t.Fatal("changed HEAD on transaction failure")
	}
	if _, err := r.panelCommit(backup); err == nil {
		t.Fatal("created backup on transaction failure")
	}
}
