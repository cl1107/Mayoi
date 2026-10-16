package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"mayoi/internal/git"
)

const branchHoverDelay = 2 * time.Second

func branchTrackingDetail(b git.Branch) string {
	switch {
	case b.Remote:
		return "Remote branch"
	case b.UpstreamRef == "":
		return "No upstream configured"
	case b.Gone:
		return "Upstream " + b.Upstream + " has been deleted"
	case b.Ahead == 0 && b.Behind == 0:
		return "Up to date with " + b.Upstream
	case b.Ahead > 0 && b.Behind > 0:
		return fmt.Sprintf("Diverged from %s · %d ahead, %d behind", b.Upstream, b.Ahead, b.Behind)
	case b.Ahead > 0:
		return fmt.Sprintf("Ahead of %s by %s", b.Upstream, plural(b.Ahead, "commit"))
	default:
		return fmt.Sprintf("Behind %s by %s", b.Upstream, plural(b.Behind, "commit"))
	}
}

func (a *app) scmBranchHover(c *ui.Context, row ui.Element, r scmRow, hovered, clicked, onButton bool) {
	p := &a.scm.panels
	if !hovered {
		if p.hoverKey == r.key {
			p.hoverKey = ""
		}
		return
	}
	now := c.Now()
	if p.hoverKey != r.key || a.frame > p.hoverFrame+1 {
		p.hoverKey, p.hoverSince, p.hoverDismissed = r.key, now, false
	}
	p.hoverFrame = a.frame
	if clicked || row.Pressed() {
		p.hoverDismissed = true
	}
	if p.hoverDismissed {
		return
	}
	if onButton {
		p.hoverSince = now
		return
	}
	if remaining := branchHoverDelay - now.Sub(p.hoverSince); remaining > 0 {
		c.After(remaining)
		return
	}
	b := *r.panel.branch
	ui.Overlay(c, func() {
		tip := ui.Column(c).Absolute().PassThrough().Role(ui.RoleTooltip).
			AttachTo(row, ui.AnchorBottom, ui.AnchorTop).Margin(4, 0, 0, 0)
		a.floating(c, tip, false).Padding(10, 12).Radius(6).Gap(8).MaxWidth(380)
		if tip.OverlayShortcut(0, ui.KeyEscape) {
			p.hoverDismissed = true
			return
		}
		tip.Children(func() {
			title := b.Name
			if b.Current {
				title += " (current branch)"
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconBranch).FontSize(14).TextColor(a.pal.muted)
				ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(a.pal.text).Shrink(1)
			})
			ui.Text(c, branchTrackingDetail(b)).FontSize(12).TextColor(a.pal.text)
			last := "Last commit time unavailable"
			if !b.Time.IsZero() {
				last = "Last commit " + ago(b.Time, time.Now()) + " (" + b.Time.Local().Format("Jan 2, 2006 15:04:05 MST") + ")"
			}
			ui.Text(c, last).FontSize(11.5).TextColor(a.pal.muted)
		})
	})
}

func (a *app) checkoutPanelBranch(b git.Branch) {
	if a.git.busy != "" || b.Current || a.git.status != nil && a.git.status.Op != "" {
		return
	}
	if !b.Remote {
		a.switchBranch(b.Name, false)
		return
	}
	if !a.docsSaved("switching branches") {
		return
	}
	_, name, ok := strings.Cut(b.Name, "/")
	if !ok {
		a.fail("Invalid remote branch %q", b.Name)
		return
	}
	a.gitDo("Checking out…", func(repo *git.Repo) error { return repo.CheckoutTracking(b.Name, name) }, nil)
}

func (a *app) pullPanelBranch(b git.Branch) {
	if a.git.busy != "" || b.Remote || b.UpstreamRef == "" || b.Gone || a.git.status != nil && a.git.status.Op != "" {
		return
	}
	if b.Current && !a.docsSaved("pulling a branch") {
		return
	}
	a.gitDo("Pulling "+b.Name+"…", func(repo *git.Repo) error { return repo.PullBranch(b.Name) }, nil)
}
