package tviewui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
)

// TestAppCtrlAOpensAddVaultModal: Ctrl+A in host mode opens the runtime
// add-vault form; while open, global keys are routed to the modal.
func TestAppCtrlAOpensAddVaultModal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModNone))
	if !app.InAddVaultModal() {
		t.Fatal("expected add-vault modal after Ctrl+A")
	}
}

// TestAppAddVaultModalCancelCloses: cancel dismisses the form with no state
// change to the vault list.
func TestAppAddVaultModalCancelCloses(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	app.ShowAddVaultModalForTest()
	app.AddVaultModal().Cancel()

	if app.InAddVaultModal() {
		t.Fatal("expected modal closed after cancel")
	}
	if len(app.Vaults()) != 0 {
		t.Fatalf("vaults changed on cancel: %+v", app.Vaults())
	}
}

// TestAppApplyAddedVaultMergesLiveAndPersists: a verified vault is appended to
// the live client (append-only), added to the configured list, persisted via
// config.AddVault, and advertised in the top bar.
func TestAppApplyAddedVaultMergesLiveAndPersists(t *testing.T) {
	hl := sampleHostList()
	cfg := config.Default()
	cfg.Vaults = []config.Vault{{Name: "vw", Server: "https://a", Email: "a@x"}}
	fc := vault.NewFakeClient()
	app := tviewui.New(hl, tviewui.Deps{VaultCli: fc, Config: cfg, ConfigPath: "/tmp/test.json"}, cfg.Vaults)

	persisted := []config.Vault(nil)
	tviewui.SetConfigAddVaultForTest(func(c *config.Config, v config.Vault, _ config.SavePathFn, _ string) error {
		c.Vaults = append(c.Vaults, v)
		persisted = append(persisted, v)
		return nil
	})
	defer tviewui.ResetConfigAddVaultForTest()

	src := vaultadapter.NewSource("work", nil, nil, config.CustomFields{})
	app.ApplyAddedVaultForTest(src, config.Vault{Name: "work", Server: "https://b", Email: "b@x"})

	if len(persisted) != 1 || persisted[0].Name != "work" {
		t.Errorf("persisted vaults = %+v, want [work]", persisted)
	}
	srcs := fc.Sources()
	if len(srcs) != 1 || srcs[0].Name() != "work" {
		t.Errorf("client sources = %v, want [work]", srcs)
	}
	if vs := app.Vaults(); len(vs) != 2 || vs[1].Name != "work" {
		t.Errorf("app vaults = %+v, want [vw work]", vs)
	}
	if !strings.Contains(app.TopBar().RawText(), "work") {
		t.Errorf("top bar does not advertise the new vault: %q", app.TopBar().RawText())
	}
}

// TestAppApplyAddedVaultDuplicateRejected: a source whose name collides with a
// live source is rejected without touching the vault list or config.
func TestAppApplyAddedVaultDuplicateRejected(t *testing.T) {
	hl := sampleHostList()
	cfg := config.Default()
	fc := vault.NewFakeClient(vault.NewFakeSource("vw", nil))
	app := tviewui.New(hl, tviewui.Deps{VaultCli: fc, Config: cfg, ConfigPath: "/tmp/test.json"}, nil)

	persisted := false
	tviewui.SetConfigAddVaultForTest(func(*config.Config, config.Vault, config.SavePathFn, string) error {
		persisted = true
		return nil
	})
	defer tviewui.ResetConfigAddVaultForTest()

	src := vaultadapter.NewSource("vw", nil, nil, config.CustomFields{})
	app.ApplyAddedVaultForTest(src, config.Vault{Name: "vw"})

	if persisted {
		t.Error("config persisted despite duplicate source")
	}
	if len(app.Vaults()) != 0 {
		t.Errorf("vaults mutated: %+v", app.Vaults())
	}
	if len(fc.Sources()) != 1 {
		t.Errorf("client sources mutated on rejected add: %v", fc.Sources())
	}
}

// TestHelpModalAdvertisesAddVault: the host help sheet lists Ctrl+A.
func TestHelpModalAdvertisesAddVault(t *testing.T) {
	m := tviewui.NewHelpModal("host")
	if !strings.Contains(m.Text(), "Ctrl+A") || !strings.Contains(m.Text(), "Add vault") {
		t.Errorf("host help missing Ctrl+A Add vault: %q", m.Text())
	}
}

