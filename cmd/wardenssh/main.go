// Command wardenssh is the WardenSSH launcher entrypoint. v0 wiring:
//   - load ~/.ssh/wardenssh.json (non-secret config; first run -> defaults)
//   - build the merged host list from ~/.ssh/config (file source) and the
//     vault client (STUB until a live VaultWarden verifies crypto — the fake
//     client produces no items, so v0 runs file-source only)
//   - start the in-process ssh-agent on a platform pipe (ready for real key
//     loads once the vault client ships) and run the Bubble Tea launcher
//
// Real vault authentication/decryption, plus the actual ssh suspend-and-exec
// on ConnectMsg, are wired in later commits against a live sshd target.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ac-kurniawan/wardenssh/internal/app"
	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/connect"
	"github.com/ac-kurniawan/wardenssh/internal/session"
	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
	"github.com/ac-kurniawan/wardenssh/internal/tui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wardenssh:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	_ = cfg // used to drive vault client + custom_fields

	// File source: ~/.ssh/config (read-only in v0). Missing file -> nil reader.
	sshConfigPath := filepath.Join(filepath.Dir(cfgPath), "config")
	var sshConfigReader io.Reader
	if data, err := os.ReadFile(sshConfigPath); err == nil {
		sshConfigReader = bytes.NewReader(data)
	}

	var vc vault.Client = vault.NewFakeClient()

	hostList, err := app.BuildHostList(sshConfigReader, vc)
	if err != nil {
		return fmt.Errorf("build host list: %w", err)
	}

	// 1. Initialize agent keyring + listener.
	kr := sshagent.NewKeyring()
	pipePath := connect.AgentPipePath()
	l, err := sshagent.Listen(pipePath)
	if err != nil {
		return fmt.Errorf("start agent listener: %w", err)
	}
	defer l.Close()
	go sshagent.Serve(l, kr)

	// 2. Initialize session manager.
	mgr := session.NewManager()

	deps := tui.Deps{
		Agent:     kr,
		Mgr:       mgr,
		VaultCli:  vc,
		AgentPipe: pipePath,
	}

	p := tea.NewProgram(tui.NewWithDeps(hostList, deps), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}