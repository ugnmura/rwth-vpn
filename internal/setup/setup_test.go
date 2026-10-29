package setup

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ugnmura/rwth-vpn/internal/credentials"
	"github.com/zalando/go-keyring"
)

type fakeUI struct {
	inputs        []string
	choices       []int
	summary       []string
	beforeConfirm func()
}

func (f *fakeUI) Choose(label string, _ []string, _ int) (int, error) {
	if strings.Contains(label, "Save") && f.beforeConfirm != nil {
		f.beforeConfirm()
	}
	if len(f.choices) == 0 {
		return 0, io.EOF
	}
	v := f.choices[0]
	f.choices = f.choices[1:]
	return v, nil
}
func (f *fakeUI) Input(string, bool) (string, error) {
	if len(f.inputs) == 0 {
		return "", io.EOF
	}
	v := f.inputs[0]
	f.inputs = f.inputs[1:]
	return v, nil
}
func (f *fakeUI) Summary(lines []string) { f.summary = append(f.summary, lines...) }
func boolPointer(v bool) *bool           { return &v }
func fixture(t *testing.T) (Paths, credentials.Store, map[string]string) {
	t.Helper()
	paths := Paths{Dir: t.TempDir()}
	store, err := credentials.New("file", filepath.Join(paths.Dir, "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"user": "old-user", "vpn_password": "old-password", "totp_secret": "JBSWY3DPEHPK3PXP", "hpc_password": "old-hpc"} {
		if err := store.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{"storage-mode": "unencrypted\n", "setup-profile": "hpc\n"} {
		if err := os.WriteFile(filepath.Join(paths.Dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return paths, store, snapshot(t, paths)
}
func snapshot(t *testing.T, paths Paths) map[string]string {
	t.Helper()
	files, err := os.ReadDir(paths.Dir)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(paths.Dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result[f.Name()] = string(data)
	}
	return result
}
func unchanged(t *testing.T, paths Paths, before map[string]string) {
	t.Helper()
	after := snapshot(t, paths)
	if len(after) != len(before) {
		t.Fatal("created files before confirmation")
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("changed %s before confirmation", k)
		}
	}
}

func TestCancelKeepsCredentialsModeAndProfile(t *testing.T) {
	paths, _, before := fixture(t)
	ui := &fakeUI{inputs: []string{"new-user", "new-password", ""}, choices: []int{0}}
	result, err := Run(ui, paths, Options{Mode: "encrypted", HPC: boolPointer(false)})
	if err != nil || result.Saved {
		t.Fatalf("canceled setup: %+v %v", result, err)
	}
	unchanged(t, paths, before)
	if strings.Contains(strings.Join(ui.summary, "\n"), "new-password") {
		t.Fatal("password leaked into review summary")
	}
}
func TestInterruptedInputKeepsAllFiles(t *testing.T) {
	paths, _, before := fixture(t)
	ui := &fakeUI{inputs: []string{"new-user"}}
	result, err := Run(ui, paths, Options{Mode: "unencrypted", HPC: boolPointer(false)})
	if err != nil || result.Saved {
		t.Fatalf("interrupted setup: %+v %v", result, err)
	}
	unchanged(t, paths, before)
}
func TestSaveOnlyAfterLastConfirmation(t *testing.T) {
	paths, store, before := fixture(t)
	ui := &fakeUI{inputs: []string{"new-user", "new-password", ""}, choices: []int{1}, beforeConfirm: func() { unchanged(t, paths, before) }}
	result, err := Run(ui, paths, Options{Mode: "unencrypted", HPC: boolPointer(false)})
	if err != nil || !result.Saved {
		t.Fatalf("confirmed setup: %+v %v", result, err)
	}
	if value, _ := store.Get("vpn_password"); value != "new-password" {
		t.Fatal("confirmed password not saved")
	}
	if value, _ := store.Get("hpc_password"); value != "old-hpc" {
		t.Fatal("VPN-only setup removed HPC credentials")
	}
	data, _ := os.ReadFile(filepath.Join(paths.Dir, "setup-profile"))
	if string(data) != "vpn-only\n" {
		t.Fatal("confirmed profile not saved")
	}
}
func TestCanceledNewSetupCreatesNoDirectory(t *testing.T) {
	paths := Paths{Dir: filepath.Join(t.TempDir(), "not-created")}
	ui := &fakeUI{inputs: []string{"user", "password", "JBSWY3DPEHPK3PXP"}, choices: []int{0}}
	result, err := Run(ui, paths, Options{Mode: "unencrypted", HPC: boolPointer(false)})
	if err != nil || result.Saved {
		t.Fatalf("canceled new setup: %+v %v", result, err)
	}
	if _, err := os.Stat(paths.Dir); !os.IsNotExist(err) {
		t.Fatal("canceled setup created persistent state")
	}
}

func TestModeMigrationUsesConfirmedDraft(t *testing.T) {
	keyring.MockInit()
	paths, _, _ := fixture(t)
	ui := &fakeUI{inputs: []string{"", "", ""}, choices: []int{1}}
	if result, err := Run(ui, paths, Options{Mode: "encrypted", HPC: boolPointer(false)}); err != nil || !result.Saved {
		t.Fatalf("encrypt migration: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(paths.Dir, "secrets")); !os.IsNotExist(err) {
		t.Fatal("plaintext source left after confirmed migration")
	}
	encryptedBackend, err := backend(paths.Dir, "encrypted")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, _ := credentials.New(encryptedBackend, "")
	if value, err := encrypted.Get("vpn_password"); err != nil || value != "old-password" {
		t.Fatal("encrypted copy missing")
	}
	ui = &fakeUI{inputs: []string{"", "", ""}, choices: []int{1}}
	if result, err := Run(ui, paths, Options{Mode: "unencrypted", HPC: boolPointer(false)}); err != nil || !result.Saved {
		t.Fatalf("file migration: %+v %v", result, err)
	}
	file, _ := credentials.New("file", filepath.Join(paths.Dir, "secrets"))
	if value, err := file.Get("vpn_password"); err != nil || value != "old-password" {
		t.Fatal("file copy missing")
	}
	if _, err := encrypted.Get("vpn_password"); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatal("encrypted source not removed after migration")
	}
}
