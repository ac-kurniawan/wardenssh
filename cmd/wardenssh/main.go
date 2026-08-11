// Command wardenssh is the WardenSSH launcher entrypoint. v0 wiring:
//   - load ~/.ssh/wardenssh.json (non-secret config; first run -> defaults)
//   - build the merged host list from ~/.ssh/config (file source) and the
//     vault client (real vault auth via TUI setup modal, or FakeClient when
//     no vaults are configured)
//   - start the in-process ssh-agent on a platform pipe and run the Bubble
//     Tea launcher
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

	// File source: ~/.ssh/config (read-only in v0). Missing file -> nil reader.
	sshConfigPath := filepath.Join(filepath.Dir(cfgPath), "config")
	var sshConfigReader io.Reader
	if data, err := os.ReadFile(sshConfigPath); err == nil {
		sshConfigReader = bytes.NewReader(data)
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
		Agent:        kr,
		Mgr:          mgr,
		AgentPipe:    pipePath,
		CustomFields: cfg.CustomFields,
	}

	// 3. Build the initial host list (file-source only; vault hosts are
	//    merged in after the setup modal completes via VaultReadyMsg).
	hostList, err := app.BuildHostList(sshConfigReader, nil)
	if err != nil {
		return fmt.Errorf("build host list: %w", err)
	}

	// 4. Launch the TUI. If vaults are configured, start in setup mode
	//    (master password prompt). Otherwise, use FakeClient (file-only).
	var model tea.Model
	if len(cfg.Vaults) > 0 {
		model = tui.NewWithSetup(hostList, deps, cfg.Vaults)
	} else {
		deps.VaultCli = vault.NewFakeClient()
		model = tui.NewWithDeps(hostList, deps)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}

	// 5. Best-effort memory wipe on exit (keys, passwords in RAM).
	kr.Wipe()

	return nil
}