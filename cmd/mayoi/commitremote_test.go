package main

import (
	"strings"
	"testing"

	gitpkg "mayoi/internal/git"
)

func TestCommitRemoteUsesSelectedRemoteSource(t *testing.T) {
	for _, remoteName := range []string{"upstream", "team/upstream"} {
		t.Run(remoteName, func(t *testing.T) {
			hs := newHarness(t, true)
			run := hs.gitRepo()
			a := hs.a
			head := strings.TrimSpace(run("rev-parse", "HEAD"))
			run("remote", "add", "origin", "https://github.com/current/project.git")
			run("remote", "add", remoteName, "https://gitlab.com/upstream/project.git")
			run("update-ref", "refs/remotes/origin/main", head)
			run("branch", "--set-upstream-to=origin/main", "main")
			remoteCommit := strings.TrimSpace(run("commit-tree", "HEAD^{tree}", "-p", head, "-m", "Remote source commit"))
			run("update-ref", "refs/remotes/"+remoteName+"/topic", remoteCommit)
			a.run("view.scm")
			hs.settleGit()
			settleScmPanels(t, hs)
			a.switchScmPanelTab(scmRemotesTab)
			hs.settle()
			remote := findPanelRow(t, hs, "panel:remote:"+remoteName)
			a.activateScmPanelRow(remote)
			settleScmPanels(t, hs)
			branch := findPanelRow(t, hs, "panel:branch:refs/remotes/"+remoteName+"/topic")
			a.activateScmPanelRow(branch)
			settleScmPanels(t, hs)
			sourceKey := branch.key + ":history:" + remoteCommit
			var cm gitpkg.Commit
			for i, r := range a.scmRows() {
				if r.key == sourceKey {
					a.scm.row = i
					cm = *r.commit
					break
				}
			}
			if cm.Hash == "" {
				t.Fatal("remote source commit not rendered")
			}
			// A historical commit need not retain a decoration naming its remote.
			cm.Refs = nil
			if got := a.commitRemoteSource(cm); got != remoteName {
				t.Fatalf("source remote %q; want %q", got, remoteName)
			}
			a.commitRemote(cm, false, true)
			a.scm.row = 0 // Selection changes after the async action has started.
			hs.settleGit()
			want := "https://gitlab.com/upstream/project/-/commit/" + remoteCommit
			if got := hs.tt.Clipboard(); got != want {
				t.Fatalf("commit URL %q; want %q", got, want)
			}
			// Incoming commits use this local branch's upstream, even though the
			// checkout still tracks origin and older commits have no decoration.
			run("branch", "topic", head)
			run("branch", "--set-upstream-to="+remoteName+"/topic", "topic")
			hs.settleGit()
			settleScmPanels(t, hs)
			a.switchScmPanelTab(scmBranchesTab)
			hs.settle()
			local := findPanelRow(t, hs, "panel:branch:refs/heads/topic")
			a.activateScmPanelRow(local)
			settleScmPanels(t, hs)
			found := false
			for i, row := range a.scmRows() {
				if row.key == local.key+":incoming:"+remoteCommit {
					a.scm.row = i
					found = true
					break
				}
			}
			if !found {
				t.Fatal("incoming commit missing")
			}
			a.commitRemote(cm, false, true)
			hs.settleGit()
			if got := hs.tt.Clipboard(); got != want {
				t.Fatalf("incoming URL %q; want %q", got, want)
			}
		})
	}
}

func TestChooseCommitRemoteName(t *testing.T) {
	remotes := []gitpkg.Remote{{Name: "origin"}, {Name: "upstream"}, {Name: "team/upstream"}}
	branches := []gitpkg.Branch{{Name: "main", Current: true, Upstream: "origin/main"}}
	for _, tc := range []struct {
		name, source string
		refs         []gitpkg.Ref
		want         string
	}{
		{name: "explicit source", source: "upstream", refs: []gitpkg.Ref{{Name: "origin/main", Kind: gitpkg.RefRemote}}, want: "upstream"},
		{name: "unique decoration", refs: []gitpkg.Ref{{Name: "upstream/topic", Kind: gitpkg.RefRemote}}, want: "upstream"},
		{name: "multiple refs same remote", refs: []gitpkg.Ref{{Name: "upstream/a", Kind: gitpkg.RefRemote}, {Name: "upstream/b", Kind: gitpkg.RefRemote}}, want: "upstream"},
		{name: "ambiguous decoration", refs: []gitpkg.Ref{{Name: "upstream/topic", Kind: gitpkg.RefRemote}, {Name: "origin/main", Kind: gitpkg.RefRemote}}, want: "origin"},
		{name: "slash in remote", refs: []gitpkg.Ref{{Name: "team/upstream/topic", Kind: gitpkg.RefRemote}}, want: "team/upstream"},
		{name: "local ref does not imply remote", refs: []gitpkg.Ref{{Name: "upstream/topic", Kind: gitpkg.RefBranch}}, want: "origin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseCommitRemoteName(tc.source, gitpkg.Commit{Refs: tc.refs}, branches, remotes); got != tc.want {
				t.Fatalf("remote %q; want %q", got, tc.want)
			}
		})
	}
	branches[0].Upstream = "team/upstream/main"
	if got := chooseCommitRemoteName("", gitpkg.Commit{}, branches, remotes); got != "team/upstream" {
		t.Fatalf("upstream remote with slash: %q", got)
	}
}
