package git

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"mayoi/internal/remote"
	"mayoi/internal/vfs"
)

// CommitPatch returns a binary-capable patch against the first parent. A root
// commit is compared with the empty tree.
func (r *Repo) CommitPatch(hash string) (string, error) {
	hash, err := r.panelCommit(hash)
	if err != nil {
		return "", err
	}
	parents, err := r.run(nil, "rev-list", "--parents", "-n", "1", hash, "--")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(parents))
	args := []string{"diff-tree", "--root", "--no-commit-id", "--binary", "--no-ext-diff", "--no-textconv", "-p", "-r"}
	if len(fields) > 1 {
		args = append(args, fields[1])
	}
	args = append(args, hash, "--")
	out, err := r.run(nil, args...)
	return string(out), err
}

// ComparePatch compares from to to, or to the tracked working tree when to is
// empty. Both staged and unstaged changes are included in that working tree.
func (r *Repo) ComparePatch(from, to string) (string, error) {
	a, err := r.panelCommit(from)
	if err != nil {
		return "", err
	}
	args := []string{"diff", "--binary", "--no-ext-diff", "--no-textconv", a}
	if to != "" {
		b, err := r.panelCommit(to)
		if err != nil {
			return "", err
		}
		args = append(args, b)
	}
	out, err := r.run(nil, append(args, "--")...)
	return string(out), err
}

// RemoteWebURL maps known public hosting services to credential-free HTTPS
// links. hash is a commit ID, never a branch name or a URL path.
func RemoteWebURL(rawURL, hash string, tree bool) (string, error) {
	if len(hash) != 40 && len(hash) != 64 {
		return "", fmt.Errorf("invalid commit ID")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", fmt.Errorf("invalid commit ID")
	}
	if !strings.Contains(rawURL, "://") {
		left, right, ok := strings.Cut(rawURL, ":")
		if !ok || !strings.Contains(left, "@") {
			return "", fmt.Errorf("unsupported remote URL")
		}
		rawURL = "ssh://" + left + "/" + right
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid remote URL")
	}
	if u.Scheme != "https" && u.Scheme != "ssh" {
		return "", fmt.Errorf("unsupported remote URL scheme")
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "gitlab.com" && host != "bitbucket.org" {
		return "", fmt.Errorf("unsupported remote hosting service")
	}
	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid repository path")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid repository path")
		}
	}
	segment := "/commit/"
	if tree {
		segment = "/tree/"
	}
	switch host {
	case "gitlab.com":
		segment = "/-" + segment
	case "bitbucket.org":
		segment = "/commits/"
		if tree {
			segment = "/src/"
		}
	}
	return (&url.URL{Scheme: "https", Host: host, Path: path + segment + hash}).String(), nil
}

// AddWorktree creates a detached checkout. Remote editor paths must refer to
// the same host as the repository and are passed to Git in that host's spelling.
func (r *Repo) AddWorktree(path, hash string) error {
	if err := panelArgument("worktree path", path); err != nil {
		return err
	}
	if host, remotePath, ok := remote.Split(path); ok {
		if host != remote.HostOf(r.Root) {
			return fmt.Errorf("worktree must be on the repository host")
		}
		path = remotePath
	}
	hash, err := r.panelCommit(hash)
	if err != nil {
		return err
	}
	_, err = r.run(nil, "worktree", "add", "--detach", "--", path, hash)
	return err
}

// Recompose stages all changes from the selected commit through HEAD for a new
// commit. It retains the old HEAD under backup and returns full messages in
// oldest-to-newest order. Ref updates are atomic, with the original branch ref
// checked against its validated ID; index and working tree remain intact.
func (r *Repo) Recompose(hash string) (backup string, message string, err error) {
	hash, err = r.panelCommit(hash)
	if err != nil {
		return "", "", err
	}
	// Capture the symbolic target independently of its object ID. Another Git
	// client can switch to a different branch at exactly the same commit.
	headRef, err := r.run(nil, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return "", "", err
	}
	headTarget := strings.TrimSpace(string(headRef))
	st, err := r.Status()
	if err != nil {
		return "", "", err
	}
	if len(st.Entries) != 0 {
		return "", "", fmt.Errorf("recompose requires a clean working tree, including untracked files")
	}
	if st.Op != "" {
		return "", "", fmt.Errorf("cannot recompose during %s", st.Op)
	}
	paths := []string{}
	for _, name := range []string{"rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "MERGE_HEAD", "sequencer"} {
		paths = append(paths, filepath.Join(r.Dir, name))
	}
	there, err := vfs.StatMany(paths)
	if err != nil {
		return "", "", err
	}
	if len(there) != 0 {
		return "", "", fmt.Errorf("cannot recompose during an in-progress operation")
	}
	parent, err := r.panelCommit(hash + "^1")
	if err != nil {
		return "", "", fmt.Errorf("recompose requires a non-root commit: %w", err)
	}
	rows, err := r.run(nil, "rev-list", "--parents", parent+".."+st.Head, "--")
	if err != nil {
		return "", "", err
	}
	found := false
	for _, row := range outputLines(rows) {
		fields := strings.Fields(row)
		if len(fields) != 2 {
			return "", "", fmt.Errorf("recompose requires linear history without merges")
		}
		if fields[0] == hash {
			found = true
		}
	}
	if !found {
		return "", "", fmt.Errorf("selected commit is not an ancestor of HEAD")
	}
	msgs, err := r.run(nil, "log", "--reverse", "--format=%B%x00", parent+".."+st.Head, "--")
	if err != nil {
		return "", "", err
	}
	var messages []string
	for _, msg := range strings.Split(string(msgs), "\x00") {
		if msg = strings.TrimSpace(msg); msg != "" {
			messages = append(messages, msg)
		}
	}
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", "", err
	}
	backup = "refs/mayoi/recompose/" + hex.EncodeToString(suffix[:])
	if err := r.checkRecomposeHeadTarget(headTarget); err != nil {
		return "", "", err
	}
	transaction := recomposeTransaction(headTarget, st.Head, parent, backup)
	if _, err := r.run([]byte(transaction), "update-ref", "-m", "recompose: soft reset", "--stdin"); err != nil {
		return "", "", fmt.Errorf("recompose could not update the original branch: %w", err)
	}
	return backup, strings.Join(messages, "\n\n"), nil
}

func (r *Repo) checkRecomposeHeadTarget(expected string) error {
	out, err := r.run(nil, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != expected {
		return fmt.Errorf("HEAD changed branches while preparing recompose; retry on the intended branch")
	}
	return nil
}

// recomposeTransaction updates the original full branch ref with an object-ID
// CAS. A concurrent checkout can never redirect this update to another branch.
// HEAD's symbolic target is rechecked before the transaction, not atomically:
// Git's files backend cannot combine symref-verify HEAD and its branch update.
func recomposeTransaction(headTarget, oldHead, parent, backup string) string {
	target := headTarget
	verify := ""
	if headTarget == "HEAD" {
		// A detached HEAD is updated directly, without following a newly created
		// symbolic ref to an unrelated branch.
		verify = "option no-deref\n"
	}
	return fmt.Sprintf("start\ncreate %s %s\n%supdate %s %s %s\nupdate ORIG_HEAD %s\nprepare\ncommit\n", backup, oldHead, verify, target, parent, oldHead, oldHead)
}
