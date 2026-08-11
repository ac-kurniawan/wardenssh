// Package sshconfig reads ~/.ssh/config (read-only in v0 per .local/spec.md
// Q6/A) and extracts launchable host entries for the host-list source "file"
// (Q10/C, Q27/C). Parsing/inheritance/wildcards/Include are delegated to
// github.com/kevinburke/ssh_config -- do not hand-roll the grammar.
package sshconfig

import (
	"io"
	"strings"

	ssh_config "github.com/kevinburke/ssh_config"
)

// Host is a single ~launchable host entry derived from an ~/.ssh/config Host
// block. Port and User are strings as in ssh_config (not numeric).
type Host struct {
	// Alias is the Host pattern string (multi-pattern blocks are kept as the
	// joined space-separated string, e.g. "bastion web-jump"); used as the
	// display label for the file source.
	Alias string
	// HostName, User, Port, ProxyJump, IdentityFile are resolved via
	// ssh_config.Get, so wildcard `Host *` defaults and Includes are honored.
	HostName     string
	User         string
	Port         string
	ProxyJump    string
	IdentityFile string
	// Wildcard marks blocks whose pattern contains '*'; these carry defaults,
	// not endpoints, and the launcher should not show them as connectable.
	Wildcard bool
}

// Parse reads an ssh config from r and returns one Host entry per Host block
// (wildcards flagged, not dropped). Concrete (non-wildcard) entries have their
// connection directives resolved through ssh_config.Get for inheritance.
func Parse(r io.Reader) ([]Host, error) {
	cfg, err := ssh_config.Decode(r)
	if err != nil {
		return nil, err
	}
	var out []Host
	for _, h := range cfg.Hosts {
		if len(h.Patterns) == 0 {
			continue
		}
		var pats []string
		for _, p := range h.Patterns {
			pats = append(pats, p.String())
		}
		alias := strings.Join(pats, " ")
		wild := strings.Contains(alias, "*")
		entry := Host{Alias: alias, Wildcard: wild}
		if !wild {
			resolve := firstNonWildcard(pats)
			entry.HostName, _ = cfg.Get(resolve, "HostName")
			entry.User, _ = cfg.Get(resolve, "User")
			entry.Port, _ = cfg.Get(resolve, "Port")
			entry.ProxyJump, _ = cfg.Get(resolve, "ProxyJump")
			entry.IdentityFile, _ = cfg.Get(resolve, "IdentityFile")
		}
		out = append(out, entry)
	}
	return out, nil
}

// firstNonWildcard returns the first pattern (from the list returned by the
// parser, joined as multi-pattern aliases) that is not a negation and not a
// wildcard, for use with ssh_config.Get on multi-pattern blocks. Falls back to
// the joined alias if none qualify.
func firstNonWildcard(pats []string) string {
	for _, p := range pats {
		if strings.HasPrefix(p, "!") {
			continue
		}
		if !strings.Contains(p, "*") {
			return p
		}
	}
	return strings.Join(pats, " ")
}