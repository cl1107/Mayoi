package main

import (
	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
	"mayoi/internal/highlight"
	"mayoi/internal/vfs"
)

// commitTools keeps the selected comparison tied to its repository.
type commitTools struct {
	repo     *git.Repo
	selected string
}

func (a *app) commitToolsMenu(m *ui.Menu, cm git.Commit, head bool, action *func()) {
	m.Separator()
	if m.Item("Create Patch…").Chosen() {
		*action = func() { a.commitPatch(cm, true) }
	}
	if m.Item("Copy Changes (Patch)").Chosen() {
		*action = func() { a.commitPatch(cm, false) }
	}
	m.Separator()
	// HEAD compared with itself, or a commit with itself, has nothing to show.
	if m.Item("Compare HEAD to Here").Disabled(head).Chosen() {
		*action = func() { a.compareRevisions("HEAD", cm.Hash) }
	}
	if m.Item("Compare Here to HEAD").Disabled(head).Chosen() {
		*action = func() { a.compareRevisions(cm.Hash, "HEAD") }
	}
	if m.Item("Select for Compare").Chosen() {
		a.commitTools = commitTools{repo: a.git.repo, selected: cm.Hash}
	}
	selected := a.commitTools
	if selected.repo == a.git.repo && selected.selected != "" {
		if m.Item("Compare with Selected (" + git.ShortHash(selected.selected) + ")").Disabled(selected.selected == cm.Hash).Chosen() {
			*action = func() { a.compareRevisions(selected.selected, cm.Hash) }
		}
	}
	m.Separator()
	if m.Item("Open Commit on Remote").Chosen() {
		*action = func() { a.commitRemote(cm) }
	}
}

// revisionName is how a revision of a comparison is named: a commit by
// its short hash, HEAD as it is.
func revisionName(rev string) string {
	if rev == "HEAD" {
		return rev
	}
	return git.ShortHash(rev)
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
		name := revisionName(from) + " → " + revisionName(to)
		if patch == "" {
			a.tell(note{message: "No differences: " + name})
			return
		}
		a.openGitText(name+".diff", patch)
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
			a.tell(note{message: "Copied the patch of " + cm.Short()})
			return
		}
		a.askSavePath(cm.Short()+".patch", func(path string) {
			if path == "" || a.git.repo != r {
				return
			}
			a.gitRead("Saving patch…", func(_ *git.Repo) error { _, err := vfs.WriteFile(path, []byte(patch), 0o644); return err }, func() { a.tell(note{message: "Patch saved to " + hostPath(path)}) })
		})
	})
}

func (a *app) commitRemote(cm git.Commit) {
	var link string
	a.gitRead("Reading remote…", func(repo *git.Repo) (err error) { link, err = repo.CommitRemoteURL(cm); return }, func() { a.services.OpenURL(link) })
}

// gitRead runs without invalidating status or loaded history. Results and
// pending counters belong to the repository captured when the read starts.
func (a *app) gitRead(busy string, fn func(*git.Repo) error, then func()) {
	g := &a.git
	r := g.repo
	if r == nil {
		return
	}
	if !a.online() {
		a.fail("Not connected to %s", a.conn.Host)
		return
	}
	if g.busy != "" {
		return
	}
	g.busy = busy
	g.pending++
	go func() {
		err := fn(r)
		a.post(func() {
			if g.repo != r {
				return
			}
			g.pending--
			g.busy = ""
			if err != nil {
				a.gitFailed(err)
				return
			}
			if then != nil {
				then()
			}
		})
	}()
}
