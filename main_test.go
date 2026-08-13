package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestNoKeyringFlagDefault(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"wardenssh"}

	showVersion, noKeyring := parseFlags()
	if showVersion {
		t.Error("expected showVersion to default to false")
	}
	if noKeyring {
		t.Error("expected noKeyring to default to false")
	}
}

func TestNoKeyringFlagParsed(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"wardenssh", "--no-keyring"}

	showVersion, noKeyring := parseFlags()
	if showVersion {
		t.Error("expected showVersion to be false with --no-keyring")
	}
	if !noKeyring {
		t.Error("expected noKeyring to be true when --no-keyring is set")
	}
}

// TestVersionFlagDetected: -version must be detected by parseFlags so main can
// print the banner (goreleaser injects main.version via ldflags) and exit
// without launching the TUI.
func TestVersionFlagDetected(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"wardenssh", "-version"}

	showVersion, noKeyring := parseFlags()
	if !showVersion {
		t.Fatal("expected showVersion to be true after -version")
	}
	if noKeyring {
		t.Error("expected noKeyring to be false with -version")
	}
}

// TestVersionBanner: the version banner contains the binary name and version.
func TestVersionBanner(t *testing.T) {
	old := versionOutput
	defer func() { versionOutput = old }()
	var out bytes.Buffer
	versionOutput = &out

	// Simulate what main() does when showVersion is set.
	printVersion()

	if !strings.Contains(out.String(), "wardenssh") {
		t.Errorf("banner = %q, want it to contain 'wardenssh'", out.String())
	}
	if !strings.Contains(out.String(), version) {
		t.Errorf("banner = %q, want it to contain version %q", out.String(), version)
	}
}
