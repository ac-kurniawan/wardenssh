package main

import (
	"os"
	"testing"
)

func TestNoKeyringFlagDefault(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"wardenssh"}

	noKeyring := parseFlags()
	if noKeyring {
		t.Error("expected noKeyring to default to false")
	}
}

func TestNoKeyringFlagParsed(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"wardenssh", "--no-keyring"}

	noKeyring := parseFlags()
	if !noKeyring {
		t.Error("expected noKeyring to be true when --no-keyring is set")
	}
}
