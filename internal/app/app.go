// Package app wires WardenSSH's subsystems together at the front door: it
// builds the launcher's merged host list from the two sources — a parsed
// ~/.ssh/config (file source, read-only per Q6/A) and the vault client's
// sources (Q10/C, Q16/B multi-vault) — applying the no-dedup + source-label
// policy (Q11/C) and the wildcard exclusion for ssh_config defaults.
package app

import (
	"io"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/sshconfig"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
)

// authKind maps a vault item kind to a hosts.Entry auth kind.
func authKind(itemKind string) string {
	if itemKind == "login" {
		return "password"
	}
	return "key"
}

// BuildHostList merges the file source (sshConfig reader; nil = no ~/.ssh/config,
// first-run) with the vault sources (nil client = no vaults yet), returning the
// unified, no-dedup host list tagged with source labels.
func BuildHostList(sshConfig io.Reader, vc vault.Client) (*hosts.List, error) {
	var entries []hosts.Entry

	if sshConfig != nil {
		hs, err := sshconfig.Parse(sshConfig)
		if err != nil {
			return nil, err
		}
		for _, h := range hs {
			if h.Wildcard {
				continue // Host * carries defaults, not an endpoint
			}
			entries = append(entries, hosts.Entry{
				Alias:        h.Alias,
				HostName:     h.HostName,
				User:         h.User,
				Port:         h.Port,
				ProxyJump:    h.ProxyJump,
				Source:       "file",
				IdentityFile: h.IdentityFile,
			})
		}
	}

	if vc != nil {
		for _, src := range vc.Sources() {
			items, err := src.Items()
			if err != nil {
				return nil, err
			}
			for _, it := range items {
				entries = append(entries, hosts.Entry{
					Alias:     it.Name, // Q30/A: BitWarden item name as display label
					HostName:  it.HostName,
					User:      it.User,
					Port:      it.Port,
					ProxyJump: it.ProxyJump,
					Source:    src.Name(),
					AuthKind:  authKind(it.Kind),
				})
			}
		}
	}

	return hosts.NewList(entries), nil
}

// VaultEntries extracts host entries from a vault client's sources. Used by
// the TUI to merge vault hosts into the existing list after setup completes.
func VaultEntries(vc vault.Client) ([]hosts.Entry, error) {
	var entries []hosts.Entry
	for _, src := range vc.Sources() {
		items, err := src.Items()
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			entries = append(entries, hosts.Entry{
				Alias:     it.Name,
				HostName:  it.HostName,
				User:      it.User,
				Port:      it.Port,
				ProxyJump: it.ProxyJump,
				Source:    src.Name(),
				AuthKind:  authKind(it.Kind),
			})
		}
	}
	return entries, nil
}
