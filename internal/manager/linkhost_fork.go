package manager

import (
	"strings"

	"github.com/knadh/listmonk/models"
)

// FORK(hcm): per-brand link host. Plan:
// docs/superpowers/plans/2026-11-10-brand-link-host.md.

// resolveLinks sets c.LinkRoot and c.LinkErr once per compile. A nil campaign
// (template previews) or a nil resolver keeps the instance root URL.
func (m *Manager) resolveLinks(c *models.Campaign) {
	if c == nil {
		return
	}
	c.LinkRoot, c.LinkErr = "", nil
	if m.cfg.LinkHostFor == nil {
		return
	}
	c.LinkRoot, c.LinkErr = m.cfg.LinkHostFor(c)
	if c.LinkErr != nil {
		c.LinkRoot = ""
	}
}

// linkFmt returns format f with the instance root replaced by the campaign's
// link host. Every format starts with RootURL (cmd/init.go initUrlConfig).
func (m *Manager) linkFmt(c *models.Campaign, f string) string {
	if c == nil || c.LinkRoot == "" || m.cfg.RootURL == "" || !strings.HasPrefix(f, m.cfg.RootURL) {
		return f
	}
	return c.LinkRoot + strings.TrimPrefix(f, m.cfg.RootURL)
}
