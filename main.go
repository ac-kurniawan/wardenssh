// Command wardenssh is the WardenSSH launcher entrypoint. v0 wiring:
//   - load ~/.ssh/wardenssh.json (non-secret config; first run -> defaults)
//   - build the merged host list from ~/.ssh/config (file source) and the
//     vault client (real vault auth via TUI setup modal, or FakeClient when
//     no vaults are configured)
//   - start the in-process ssh-agent on a platform pipe and run the tview/tvxterm
//     launcher
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ac-kurniawan/wardenssh/internal/app"
	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/connect"
	"github.com/ac-kurniawan/wardenssh/internal/session"
	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
)

// version is the release version, injected at build time by goreleaser via
// -ldflags "-X main.version={{.Version}}". Defaults to "dev" for local builds.
var version = "dev"

// versionOutput is the io.Writer for the -version banner (overridable for
// tests).
var versionOutput io.Writer = os.Stdout

// askpassOutput is the io.Writer for the SSH_ASKPASS helper mode (overridable
// for tests). ssh spawns this binary and reads the password from stdout.
var askpassOutput io.Writer = os.Stdout

func main() {
	if runAskpass() {
		return
	}
	showVersion, noKeyring := parseFlags()
	if showVersion {
		printVersion()
		return
	}
	if err := run(noKeyring); err != nil {
		fmt.Fprintln(os.Stderr, "wardenssh:", err)
		os.Exit(1)
	}
}

// runAskpass detects the SSH_ASKPASS helper mode and prints the vault password
// to stdout, exiting without touching config/vault/agent. Returns true when it
// handled the request.
func runAskpass() bool {
	if os.Getenv("WARDENSSH_ASKPASS") != "1" {
		return false
	}
	fmt.Fprint(askpassOutput, os.Getenv("WARDENSSH_ASKPASS_PASS"))
	return true
}

// printVersion writes the version banner to versionOutput.
func printVersion() {
	fmt.Fprintf(versionOutput, "wardenssh %s\n", version)
}

// parseFlags parses the command-line flags. Returns (showVersion, noKeyring).
// It is a pure flag parser; it never prints or exits.
func parseFlags() (showVersion, noKeyring bool) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	noKeyringFlag := fs.Bool("no-keyring", false, "prompt for master password interactively; do not use OS keyring")
	showVersionFlag := fs.Bool("version", false, "print version and exit")
	_ = fs.Parse(os.Args[1:])
	return *showVersionFlag, *noKeyringFlag
}

func run(noKeyring bool) error {
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

	deps := tviewui.Deps{
		Agent:        kr,
		Mgr:          mgr,
		AgentPipe:    pipePath,
		CustomFields: cfg.CustomFields,
		NoKeyring:    noKeyring,
	}

	// 3. Build the initial host list (file-source only; vault hosts are
	//    merged in after the setup modal completes).
	hostList, err := app.BuildHostList(sshConfigReader, nil)
	if err != nil {
		return fmt.Errorf("build host list: %w", err)
	}

	if len(cfg.Vaults) == 0 {
		deps.VaultCli = vault.NewFakeClient()
	}

	// 4. Launch the TUI.
	uiApp := tviewui.New(hostList, deps, cfg.Vaults)
	if err := uiApp.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}

	// 5. Best-effort memory wipe on exit (keys, passwords in RAM).
	kr.Wipe()

	return nil
}