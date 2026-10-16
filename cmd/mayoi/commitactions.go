package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// commitTools keeps the comparison tied to the repository it came from.
type commitTools struct {
	repo     *git.Repo
	selected string
}

func (a *app) commitToolsMenu(m *ui.Menu, cm git.Commit, moving bool, action *func()) {
	if m.Item("Inspect Details").Chosen() {
		*action = func() { a.showCommit(cm, true) }
	}
	if m.Item("Open in Commit Graph").Chosen() {
		*action = func() { a.revealCommit(cm) }
	}
	if m.Item("Reveal Commit in Side Bar").Chosen() {
		*action = func() { a.revealCommit(cm) }
	}
	m.Separator()
	if m.Item("Recompose Commits from Here…").Disabled(moving).Chosen() {
		*action = func() { a.recomposeCommits(cm) }
	}
	if m.Item("Create Worktree…").Disabled(moving).Chosen() {
		*action = func() { a.createCommitWorktree(cm) }
	}
	if m.Item("Create Patch…").Chosen() {
		*action = func() { a.commitPatch(cm, true) }
	}
	if m.Item("Copy Changes (Patch)").Chosen() {
		*action = func() { a.commitPatch(cm, false) }
	}
	m.Separator()
	if m.Item("Compare HEAD to Here").Chosen() {
		*action = func() { a.compareRevisions("HEAD", cm.Hash) }
	}
	if m.Item("Compare Here to HEAD").Chosen() {
		*action = func() { a.compareRevisions(cm.Hash, "HEAD") }
	}
	if m.Item("Compare Working Tree to Here").Chosen() {
		*action = func() { a.compareRevisions(cm.Hash, "") }
	}
	if m.Item("Select for Compare").Chosen() {
		a.commitTools = commitTools{repo: a.git.repo, selected: cm.Hash}
	}
	selected := a.commitTools
	if selected.repo == a.git.repo && selected.selected != "" {
		if m.Item("Compare with Selected (" + git.ShortHash(selected.selected) + ")").Chosen() {
			*action = func() { a.compareRevisions(selected.selected, cm.Hash) }
		}
	}
	m.Separator()
	if m.Item("Open Commit on Remote").Chosen() {
		*action = func() { a.commitRemote(cm, false, false) }
	}
	if m.Item("Browse Repository at Commit").Chosen() {
		*action = func() { a.commitRemote(cm, true, false) }
	}
	if m.Item("Share: Copy Commit URL").Chosen() {
		*action = func() { a.commitRemote(cm, false, true) }
	}
	if m.Item("Copy").Chosen() {
		a.services.WriteClipboard(cm.Short() + " " + cm.Subject)
	}
	if m.Item("Copy SHA").Chosen() {
		a.services.WriteClipboard(cm.Hash)
	}
	if m.Item("Copy Message").Chosen() {
		a.services.WriteClipboard(commitMessage(cm))
	}
	m.Separator()
	if m.Item("Collapse").Chosen() {
		*action = func() { a.collapseCommitSource(cm, true) }
	}
	if m.Item("Expand").Chosen() {
		*action = func() { a.collapseCommitSource(cm, false) }
	}
	if m.Item("Refresh").Chosen() {
		*action = a.gitChanged
	}
	m.Separator()
}

func commitMessage(cm git.Commit) string {
	if cm.Body == "" {
		return cm.Subject
	}
	return cm.Subject + "\n\n" + cm.Body
}

// Comparisons open as a syntax-highlighted patch document, so binary changes,
// renames, and either direction retain Git's precise semantics and can be saved.
func (a *app) compareRevisions(from, to string) {
	r := a.git.repo
	if r == nil {
		return
	}
	var patch string
	a.gitRead("Comparing…", func(repo *git.Repo) (err error) { patch, err = repo.ComparePatch(from, to); return }, func() {
		if a.git.repo != r {
			return
		}
		end := to
		if end == "" {
			end = "Working Tree (tracked files)"
		}
		if patch == "" {
			a.tell(note{message: "No differences: " + from + " → " + end})
			return
		}
		a.openGitText(from+" → "+end+".diff", patch)
	})
}

func (a *app) openGitText(title, text string) {
	a.nextID++
	doc := newDocument(a.nextID, "", text, highlight.New(highlight.Lexer("changes.diff"), a.invalidate))
	doc.title = title
	a.docs = append(a.docs, doc)
	col := a.fileColumn()
	a.addTab(col, len(col.tabs), &colTab{ed: newEditor(doc)})
	a.activate(col, len(col.tabs)-1, true)
}

func (a *app) commitPatch(cm git.Commit, save bool) {
	r := a.git.repo
	if r == nil {
		return
	}
	var patch string
	a.gitRead("Reading patch…", func(repo *git.Repo) (err error) { patch, err = repo.CommitPatch(cm.Hash); return }, func() {
		if a.git.repo != r {
			return
		}
		if !save {
			a.services.WriteClipboard(patch)
			return
		}
		a.askSavePath(cm.Short()+".patch", func(path string) {
			if path == "" {
				return
			}
			a.gitDo("Saving patch…", func(_ *git.Repo) error { _, err := vfs.WriteFile(path, []byte(patch), 0o644); return err }, func() { a.tell(note{message: "Patch saved to " + hostPath(path)}) })
		})
	})
}

func (a *app) commitRemote(cm git.Commit, tree, copyURL bool) {
	r := a.git.repo
	if r == nil {
		return
	}
	// Capture the selected tree source before starting background work: row
	// selection may change while URLs and refs are being read.
	source := a.commitRemoteSource(cm)
	var link string
	a.gitRead("Reading remote…", func(repo *git.Repo) error {
		remotes, err := repo.RemoteList()
		if err != nil {
			return err
		}
		branches, err := repo.BranchList()
		if err != nil {
			return err
		}
		if len(remotes) == 0 {
			return errors.New("this repository has no remote")
		}
		preferred := chooseCommitRemoteName(source, cm, branches, remotes)
		var chosen git.Remote
		for _, rm := range remotes {
			if rm.Name == preferred {
				chosen = rm
				break
			}
		}
		if chosen.Name == "" {
			return fmt.Errorf("remote %s is no longer configured", preferred)
		}
		if len(chosen.FetchURLs) == 0 {
			return fmt.Errorf("remote %s has no fetch URL", chosen.Name)
		}
		link, err = git.RemoteWebURL(chosen.FetchURLs[0], cm.Hash, tree)
		return err
	}, func() {
		if a.git.repo != r {
			return
		}
		if copyURL {
			a.services.WriteClipboard(link)
		} else {
			a.services.OpenURL(link)
		}
	})
}

func (a *app) createCommitWorktree(cm git.Commit) {
	r := a.git.repo
	if r == nil {
		return
	}
	a.prompt = &prompt{title: "Create Worktree…", hint: "New folder; detached HEAD at " + cm.Short(), done: func(path string) string {
		path = strings.TrimSpace(path)
		if path == "" {
			return "Enter a new folder path."
		}
		if a.git.repo != r {
			return "The repository changed. Open this command again."
		}
		if remote.Is(r.Root) {
			if !strings.HasPrefix(path, "/") {
				return "Enter an absolute path on the host."
			}
		} else {
			if !filepath.IsAbs(path) {
				path = filepath.Join(r.Root, path)
			}
		}
		a.confirm = &confirm{title: "Create Worktree?", ok: "Create", message: path + "\nHEAD will be detached at " + cm.Short() + ".", run: func() {
			if a.git.repo != r {
				a.fail("The repository changed. Open this command again.")
				return
			}
			a.gitDo("Creating worktree…", func(repo *git.Repo) error { return repo.AddWorktree(path, cm.Hash) }, func() { a.tell(note{message: "Worktree created: " + path}) })
		}}
		return ""
	}}
}

func (a *app) recomposeCommits(cm git.Commit) {
	r := a.git.repo
	if r == nil {
		return
	}
	a.confirm = &confirm{title: "Recompose Commits from " + cm.Short() + "?", ok: "Prepare Changes", message: "The selected commit and every commit after it become staged changes. Edit the combined message and commit again. A recovery ref keeps the previous HEAD. Requires a clean work tree and linear history; published commits will be rewritten.", run: func() {
		if a.git.repo != r {
			a.fail("The repository changed. Open this command again.")
			return
		}
		if !a.docsSaved("recomposing commits") {
			return
		}
		var backup, message string
		a.gitDo("Preparing commits…", func(repo *git.Repo) (err error) { backup, message, err = repo.Recompose(cm.Hash); return }, func() {
			if a.git.repo != r {
				return
			}
			a.git.message, a.git.amend = message, false
			a.showSourceControl()
			a.tell(note{message: "Changes staged. Previous HEAD: " + backup, label: "Copy Recovery Ref", action: func() { a.services.WriteClipboard(backup) }})
		})
	}}
}

func (a *app) revealCommit(cm git.Commit) {
	if a.git.repo == nil {
		return
	}
	a.showSourceControl()
	a.git.historyRef = cm.Hash
	if st := a.git.status; st != nil {
		a.git.historyRefOf = st.Head + ":" + st.Branch
	}
	a.git.historyN = historyPage
	a.scm.collapsed[groupCommits] = false
	// The selected commit is the first history row after the async read.
	a.scm.revealHash = cm.Hash
	a.previewCommit(cm)
}

// commitRemoteSource identifies a remote history by its full branch-row key.
// Configured names may contain slashes, so splitting at the first slash loses
// the repository identity.
func (a *app) commitRemoteSource(cm git.Commit) string {
	p := &a.scm.panels
	if p.repo != a.git.repo {
		return ""
	}
	rows := a.scmRows()
	if a.scm.row < 0 || a.scm.row >= len(rows) {
		return ""
	}
	row := rows[a.scm.row]
	if row.commit == nil || row.commit.Hash != cm.Hash {
		return ""
	}
	for _, b := range p.branches {
		for _, source := range []string{"history", "incoming", "outgoing"} {
			if row.key != "panel:branch:"+panelRef(b)+":"+source+":"+cm.Hash {
				continue
			}
			if b.Remote {
				return commitRemoteNameForBranch(b.Name, p.remotes)
			}
			return commitRemoteNameForBranch(strings.TrimPrefix(b.UpstreamRef, "refs/remotes/"), p.remotes)
		}
	}
	return ""
}

func commitRemoteNameForBranch(branch string, remotes []git.Remote) string {
	name := ""
	for _, rm := range remotes {
		if strings.HasPrefix(branch, rm.Name+"/") && len(rm.Name) > len(name) {
			name = rm.Name
		}
	}
	return name
}

// Explicit tree context wins over decorations, which win over the checked
// out branch's upstream. Decorations are only used when they identify one
// remote unambiguously.
func chooseCommitRemoteName(source string, cm git.Commit, branches []git.Branch, remotes []git.Remote) string {
	if source != "" {
		return source
	}
	unique := map[string]bool{}
	for _, ref := range cm.Refs {
		if ref.Kind == git.RefRemote {
			if name := commitRemoteNameForBranch(ref.Name, remotes); name != "" {
				unique[name] = true
			}
		}
	}
	if len(unique) == 1 {
		for name := range unique {
			return name
		}
	}
	for _, b := range branches {
		if b.Current && b.Upstream != "" {
			if name := commitRemoteNameForBranch(b.Upstream, remotes); name != "" {
				return name
			}
		}
	}
	for _, rm := range remotes {
		if rm.Name == "origin" {
			return rm.Name
		}
	}
	if len(remotes) > 0 {
		return remotes[0].Name
	}
	return ""
}
