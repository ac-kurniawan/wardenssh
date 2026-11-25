package tviewui_test

import (
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestCopyModal_ShowsAliasDirectionAndWarning(t *testing.T) {
	modal := tviewui.NewCopyModal("prod-db", "vault → local",
		"~/.ssh/config", "Private key will be written to disk.")
	text := modal.Text()
	for _, want := range []string{"prod-db", "vault → local", "~/.ssh/config",
		"Private key will be written to disk.", "[y] Copy", "[n] Cancel"} {
		if !strings.Contains(text, want) {
			t.Errorf("copy modal %q missing %q", text, want)
		}
	}
	if got := modal.CopyTargetAlias(); got != "prod-db" {
		t.Errorf("CopyTargetAlias = %q, want prod-db", got)
	}
}

func TestCopyModal_Confirm(t *testing.T) {
	copied := false
	cancelled := false
	modal := tviewui.NewCopyModal("prod-db", "local → vault", "vw", "")
	modal.SetOnCopy(func() { copied = true })
	modal.SetOnCancel(func() { cancelled = true })

	modal.TriggerCopy()

	if !copied {
		t.Error("expected onCopy callback to fire")
	}
	if cancelled {
		t.Error("did not expect onCancel callback to fire")
	}
}

func TestCopyModal_Cancel(t *testing.T) {
	copied := false
	cancelled := false
	modal := tviewui.NewCopyModal("prod-db", "local → vault", "vw", "")
	modal.SetOnCopy(func() { copied = true })
	modal.SetOnCancel(func() { cancelled = true })

	modal.TriggerCancel()

	if copied {
		t.Error("did not expect onCopy callback to fire")
	}
	if !cancelled {
		t.Error("expected onCancel callback to fire")
	}
}
