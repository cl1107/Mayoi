package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	gitpkg "mayoi/internal/git"
)

func TestCommitToolsMenuAndComparison(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	os.WriteFile(hs.path("main.go"), []byte("package main\n// comparison result\n"), 0o644)
	run("commit", "-qam", "second commit\n\nFull message body")
	a, tt := hs.a, hs.tt
	a.run("view.scm")
	hs.settleGit()
	cm := a.git.history[0]
	if err := tt.RightClick(cm.Subject); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Create Patch…", "Create Worktree…", "Recompose Commits from Here…", "Compare HEAD to Here", "Copy SHA", "Share: Copy Commit URL", "Reveal Commit in Side Bar"} {
		if !slices.Contains(tt.Menu(), label) {
			t.Errorf("missing %q from menu %q", label, tt.Menu())
		}
	}
	if err := tt.ChooseMenuItem("Copy Message"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := tt.Clipboard(); got != "second commit\n\nFull message body" {
		t.Fatalf("message %q", got)
	}
	if err := tt.RightClick(cm.Subject); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Select for Compare"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.commitTools.selected != cm.Hash || a.commitTools.repo != a.git.repo {
		t.Fatal("comparison selection not saved")
	}
	parent := a.git.history[1]
	a.compareRevisions(parent.Hash, cm.Hash)
	hs.settleGit()
	if ed := a.activeEditor(); ed == nil || !strings.Contains(ed.buf.String(), "+// comparison result") || !strings.Contains(ed.document.title, " → ") {
		t.Fatal("missing directed comparison patch")
	}
	a.commitPatch(cm, false)
	hs.settleGit()
	if got := tt.Clipboard(); !strings.Contains(got, "+// comparison result") {
		t.Fatalf("patch %q", got)
	}
}

func TestRevealCommitAnchorsHistory(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("checkout", "-qb", "topic")
	os.WriteFile(hs.path("main.go"), []byte("package main\n// side commit\n"), 0o644)
	run("commit", "-qam", "side commit")
	hash := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("checkout", "-q", "main")
	hs.a.run("view.scm")
	hs.settleGit()
	a := hs.a
	a.revealCommit(gitpkg.Commit{Hash: hash, Subject: "side commit"})
	hs.settleGit()
	if len(a.git.history) == 0 || a.git.history[0].Hash != hash || a.git.historyRef != hash {
		t.Fatal("selected commit not in anchored history")
	}
	if a.scm.collapsed[groupCommits] {
		t.Fatal("history is collapsed")
	}
	// More pages and ref invalidations must retain the revealed source.
	a.git.historyN += historyPage
	hs.settleGit()
	if a.git.history[0].Hash != hash {
		t.Fatal("pagination lost selected branch")
	}
	a.toggleAllBranches()
	hs.settleGit()
	if a.git.historyRef != "" {
		t.Fatal("all-branches toggle did not restore regular history")
	}
	a.revealCommit(gitpkg.Commit{Hash: hash, Subject: "side commit"})
	hs.settleGit()
	run("checkout", "-q", "topic")
	hs.settleGit()
	if a.git.historyRef != "" {
		t.Fatal("switching branches kept stale anchored history")
	}
}

func TestCommitReadActionsRetainPagedHistory(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	branch := findPanelRow(t, hs, "panel:branch:refs/heads/main")
	a.activateScmPanelRow(branch)
	settleScmPanels(t, hs)
	key := branch.key + ":history"
	log := a.scm.panels.logs[key]
	log.n = 100
	a.commitPatch(a.git.history[0], false)
	for range 100 {
		hs.settle()
		if a.git.pending == 0 {
			break
		}
	}
	if a.git.pending != 0 {
		t.Fatal("patch read did not finish")
	}
	if a.scm.panels.logs[key] != log || log.n != 100 {
		t.Fatal("read-only action reset the paged history")
	}
}

func TestRevealAlreadyLoadedCommitSelectsItsNewRow(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("commit", "--allow-empty", "-qm", "second")
	run("commit", "--allow-empty", "-qm", "third")
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	cm := a.git.history[1]
	a.revealCommit(cm)
	hs.settleGit()
	rows := a.scmRows()
	if i := a.scm.row; i < 0 || i >= len(rows) || rows[i].commit == nil || rows[i].commit.Hash != cm.Hash {
		t.Fatalf("revealed commit lost selection after its history was replaced: row=%d", a.scm.row)
	}
}

func TestGitDoDoesNotApplyAfterRepositorySwitch(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	called := false
	a.gitDo("Waiting…", func(_ *gitpkg.Repo) error { close(started); <-release; close(finished); return nil }, func() { called = true })
	<-started
	a.openFolder(t.TempDir())
	close(release)
	<-finished
	for range 20 {
		hs.settle()
		time.Sleep(time.Millisecond)
	}
	if called || a.git.pending != 0 || a.git.busy != "" {
		t.Fatalf("old task changed new repository: callback=%v pending=%d busy=%q", called, a.git.pending, a.git.busy)
	}
}

func TestRecomposePreparesCommitMessage(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	os.WriteFile(hs.path("main.go"), []byte("package main\n// recompose\n"), 0o644)
	run("commit", "-qam", "message to keep")
	hs.settleGit()
	a := hs.a
	cm := a.git.history[0]
	a.recomposeCommits(cm)
	if a.confirm == nil {
		t.Fatal("recompose must ask first")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if !strings.Contains(a.git.message, "message to keep") || a.git.amend || len(a.git.status.Entries) == 0 || !a.git.status.Entries[0].Staged() {
		t.Fatalf("not prepared: message=%q status=%+v", a.git.message, a.git.status)
	}
}

func TestCommitMenuConfirmationStaysWithRepository(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	cm := a.git.history[0]
	if err := hs.tt.RightClick(cm.Subject); err != nil {
		t.Fatal(err)
	}
	if err := hs.tt.ChooseMenuItem("Revert Commit…"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.confirm == nil {
		t.Fatal("missing revert confirmation")
	}
	run := a.confirm.run
	a.confirm = nil
	// Even reopening the same folder creates a new repository session. Ensure
	// it is loaded so an unguarded callback really would start a command.
	a.openFolder(hs.root)
	hs.settleGit()
	run()
	if a.git.pending != 0 || a.git.busy != "" {
		t.Fatal("old confirmation started a command in new repository")
	}
}
