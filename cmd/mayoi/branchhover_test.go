package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
)

func TestScmBranchHoverAndCheckout(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("branch", "topic")
	a := hs.a
	a.window.SidebarWidth = 320
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	rect, ok := hs.tt.Find("topic")
	if !ok {
		t.Fatal("topic row missing")
	}
	hs.tt.Move(rect.X+5, rect.Y+rect.H/2)
	hs.tt.Frame()
	if !hs.tt.HasText("Check Out Branch") || !hs.tt.HasText("Pull Branch") {
		t.Fatal("hover actions missing")
	}
	if hs.tt.HasText("No upstream configured") {
		t.Fatal("branch card appeared before delay")
	}
	time.Sleep(1100 * time.Millisecond)
	hs.tt.Frame()
	if hs.tt.HasText("No upstream configured") {
		t.Fatal("branch card appeared before 2 seconds")
	}
	time.Sleep(1000 * time.Millisecond)
	hs.tt.Frame()
	if !hs.tt.HasText("No upstream configured") || !hs.tt.HasText("Last commit") {
		t.Fatalf("branch card missing: %q", hs.tt.Texts())
	}
	hs.shot("git-branch-hover-dark")
	hs.tt.SetDark(false)
	hs.tt.Frame()
	hs.shot("git-branch-hover-light")
	hs.tt.Key(0, ui.KeyEscape)
	hs.tt.Frame()
	if hs.tt.HasText("No upstream configured") {
		t.Fatal("Escape did not dismiss card")
	}
	hs.tt.Move(600, 300)
	hs.tt.Frame()
	if a.scm.panels.hoverKey != "" {
		t.Fatal("hover state survived pointer exit")
	}
	hs.tt.Move(rect.X+5, rect.Y+rect.H/2)
	hs.tt.Frame()
	key := "panel:branch:refs/heads/topic"
	closed := a.scm.panels.closed[key]
	if err := hs.tt.Click("Pull Branch topic"); err != nil {
		t.Fatal(err)
	}
	hs.tt.Frame()
	if a.git.busy != "" || a.scm.panels.closed[key] != closed {
		t.Fatal("disabled pull ran or toggled expansion")
	}
	if err := hs.tt.Click("Check Out Branch topic"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	settleScmPanels(t, hs)
	if a.git.status.Branch != "topic" {
		t.Fatalf("checkout selected %q", a.git.status.Branch)
	}
	if a.scm.panels.closed[key] != closed {
		t.Fatal("checkout toggled expansion")
	}
}

func TestBranchTrackingDetail(t *testing.T) {
	for _, tc := range []struct {
		branch git.Branch
		want   string
	}{
		{git.Branch{}, "No upstream configured"},
		{git.Branch{Remote: true}, "Remote branch"},
		{git.Branch{Upstream: "origin/main", UpstreamRef: "refs/remotes/origin/main"}, "Up to date with origin/main"},
		{git.Branch{Upstream: "origin/main", UpstreamRef: "ref", Gone: true}, "has been deleted"},
		{git.Branch{Upstream: "origin/main", UpstreamRef: "ref", Ahead: 2}, "Ahead of origin/main by 2 commits"},
		{git.Branch{Upstream: "origin/main", UpstreamRef: "ref", Behind: 1}, "Behind origin/main by 1 commit"},
		{git.Branch{Upstream: "origin/main", UpstreamRef: "ref", Ahead: 2, Behind: 1}, "Diverged from origin/main · 2 ahead, 1 behind"},
	} {
		if got := branchTrackingDetail(tc.branch); !strings.Contains(got, tc.want) {
			t.Errorf("%+v: %q, want %q", tc.branch, got, tc.want)
		}
	}
}

func TestScmBranchPullAction(t *testing.T) {
	hs := newHarness(t, true)
	run := hs.gitRepo()
	run("branch", "topic")
	run("remote", "add", "origin", hs.root)
	run("fetch", "origin")
	run("branch", "--set-upstream-to=origin/main", "topic")
	run("commit", "--allow-empty", "-qm", "incoming")
	a := hs.a
	a.run("view.scm")
	hs.settleGit()
	settleScmPanels(t, hs)
	rect, ok := hs.tt.Find("topic")
	if !ok {
		t.Fatal("topic missing")
	}
	hs.tt.Move(rect.X+3, rect.Y+rect.H/2)
	hs.tt.Frame()
	key := "panel:branch:refs/heads/topic"
	closed := a.scm.panels.closed[key]
	if err := hs.tt.Click("Pull Branch topic"); err != nil {
		t.Fatal(err)
	}
	hs.settleGit()
	settleScmPanels(t, hs)
	if got, want := strings.TrimSpace(run("rev-parse", "topic")), strings.TrimSpace(run("rev-parse", "main")); got != want {
		t.Fatalf("pull did not move selected branch: %s != %s", got, want)
	}
	if a.git.status.Branch != "main" || a.scm.panels.closed[key] != closed {
		t.Fatal("pull switched branch or changed expansion")
	}
}

// The remote advances after the expanded history was cached, as it does when
// another developer pushes while the Repository panel remains open.
func TestScmRemoteUpdatesFromPanel(t *testing.T) {
	for _, button := range []string{"Refresh", "Fetch Branch origin/main"} {
		t.Run(button, func(t *testing.T) {
			hs := newHarness(t, true)
			run := hs.gitRepo()
			remote := filepath.Join(t.TempDir(), "remote.git")
			run("clone", "--bare", hs.root, remote)
			run("remote", "add", "origin", remote)
			run("fetch", "origin")
			run("branch", "--set-upstream-to=origin/main", "main")
			a := hs.a
			a.run("view.scm")
			hs.settleGit()
			settleScmPanels(t, hs)
			a.switchScmPanelTab(scmRemotesTab)
			hs.settle()
			a.activateScmPanelRow(findPanelRow(t, hs, "panel:remote:origin"))
			hs.settle()
			key := "panel:branch:refs/remotes/origin/main"
			a.activateScmPanelRow(findPanelRow(t, hs, key))
			settleScmPanels(t, hs)
			before := strings.TrimSpace(run("rev-parse", "origin/main"))
			run("commit", "--allow-empty", "-qm", "new remote update")
			want := strings.TrimSpace(run("rev-parse", "main"))
			run("push", "origin", "main")
			run("reset", "--hard", before)
			// Push updates the sender's tracking ref; restore the stale view
			// that a separate clone would have before fetching.
			run("update-ref", "refs/remotes/origin/main", before)
			if before == want {
				t.Fatal("fixture did not advance remote")
			}
			rect, ok := hs.tt.Find("origin/main")
			if !ok {
				t.Fatal("remote branch missing")
			}
			hs.tt.Move(rect.X+3, rect.Y+rect.H/2)
			hs.tt.Frame()
			if err := hs.tt.Click(button); err != nil {
				t.Fatal(err)
			}
			// Do not invalidate state from the test: the clicked action must
			// refresh the metadata and expanded-history caches itself.
			for range 40 {
				hs.settle()
				if a.git.busy == "" && a.git.pending == 0 && !a.git.stale && !a.git.running {
					break
				}
			}
			settleScmPanels(t, hs)
			if got := strings.TrimSpace(run("rev-parse", "origin/main")); got != want {
				t.Fatalf("%s left remote ref stale: got %s, want %s", button, got, want)
			}
			row := findPanelRow(t, hs, key)
			if row.panel.branch.Hash != want {
				t.Fatal("branch metadata stayed stale")
			}
			log := a.scm.panels.logs[key+":history"]
			if log == nil || len(log.commits) == 0 || log.commits[0].Hash != want {
				t.Fatalf("expanded remote history stayed stale: %+v", log)
			}
			if a.git.status.Branch != "main" || strings.TrimSpace(run("rev-parse", "main")) != before {
				t.Fatal("fetch changed local branch")
			}
			if a.git.status.Behind != 1 {
				t.Fatalf("current branch still reports stale tracking status: %+v", a.git.status)
			}
		})
	}
}
