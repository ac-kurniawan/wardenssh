package tviewui_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestDeleteModal_Confirm(t *testing.T) {
	deleted := false
	cancelled := false

	modal := tviewui.NewDeleteModal("prod-db", "~/.ssh/config", false)
	modal.SetOnDelete(func() { deleted = true })
	modal.SetOnCancel(func() { cancelled = true })

	modal.TriggerDelete()

	if !deleted {
		t.Error("expected onDelete callback to fire")
	}
	if cancelled {
		t.Error("did not expect onCancel callback to fire")
	}
}

func TestDeleteModal_Cancel(t *testing.T) {
	deleted := false
	cancelled := false

	modal := tviewui.NewDeleteModal("prod-db", "vw:personal", false)
	modal.SetOnDelete(func() { deleted = true })
	modal.SetOnCancel(func() { cancelled = true })

	modal.TriggerCancel()

	if deleted {
		t.Error("did not expect onDelete callback to fire")
	}
	if !cancelled {
		t.Error("expected onCancel callback to fire")
	}
}
