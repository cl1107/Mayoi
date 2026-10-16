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
	for _, label := range []string{"Create Patch…", "Copy Changes (Patch)", "Compare HEAD to Here", "Compare Here to HEAD", "Select for Compare", "Open Commit on Remote"} {
		if !slices.Contains(tt.Menu(), label) {
			t.Errorf("missing %q from menu %q", label, tt.Menu())
		}
	}
	for _, label := range []string{"Inspect Details", "Open in Commit Graph", "Reveal Commit in Side Bar", "Recompose Commits from Here…", "Create Worktree…", "Copy SHA", "Copy Message", "Collapse", "Expand", "Refresh"} {
		if slices.Contains(tt.Menu(), label) {
			t.Errorf("unexpected %q in commit menu", label)
		}
	}
	if slices.Index(tt.Menu(), "Create Patch…") < slices.Index(tt.Menu(), "Undo Last Commit") {
		t.Fatal("tools precede existing commit actions")
	}

	if err := tt.ChooseMenuItem("Copy Commit Message"); err != nil {
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
	if err := tt.RightClick(parent.Subject); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Compare with Selected (" + cm.Short() + ")"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	if ed := a.activeEditor(); ed == nil || !strings.Contains(ed.buf.String(), "-// comparison result") || !strings.Contains(ed.document.title, " → ") {
		t.Fatal("missing directed comparison patch")
	}
	if got, want := a.activeEditor().document.title, cm.Short()+" → "+parent.Short()+".diff"; got != want {
		t.Fatalf("comparison named %q, want %q", got, want)
	}
	a.commitPatch(cm, false)
	hs.settleGit()
	if got := tt.Clipboard(); !strings.Contains(got, "+// comparison result") {
		t.Fatalf("patch %q", got)
	}
}

func TestCommitPatchExportAndRemote(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	cm := a.git.history[0]
	hs.h.saveTo = hs.path("export.patch")
	a.commitPatch(cm, true)
	hs.settleGit()
	patch, err := os.ReadFile(hs.h.saveTo)
	if err != nil || !strings.Contains(string(patch), "new file mode") {
		t.Fatalf("exported patch %q: %v", patch, err)
	}
	run("remote", "add", "origin", "git@github.com:owner/repo.git")
	a.commitRemote(cm)
	hs.settleGit()
	if got := hs.tt.OpenedURLs(); !slices.Equal(got, []string{"https://github.com/owner/repo/commit/" + cm.Hash}) {
		t.Fatalf("opened URLs %q", got)
	}
}

func TestGitReadDiscardsOldRepositoryResultWithoutChangingNewTask(t *testing.T) {
	old := &gitpkg.Repo{}
	a := &app{git: gitState{repo: old}}
	release := make(chan struct{})
	called := false
	a.gitRead("Old read", func(r *gitpkg.Repo) error {
		if r != old {
			t.Error("read received wrong repository")
		}
		<-release
		return nil
	}, func() { called = true })
	a.git = gitState{repo: &gitpkg.Repo{}, pending: 1, busy: "New read"}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		a.mu.Lock()
		queue := a.queue
		a.queue = nil
		a.mu.Unlock()
		if len(queue) != 0 {
			for _, fn := range queue {
				fn()
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("read never posted its result")
		}
		time.Sleep(time.Millisecond)
	}
	if called || a.git.pending != 1 || a.git.busy != "New read" {
		t.Fatalf("old read changed new task: callback %v, pending %d, busy %q", called, a.git.pending, a.git.busy)
	}
}
