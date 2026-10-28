package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestFileCredentialsRoundTrip(t *testing.T) {
	s := fileStore{filepath.Join(t.TempDir(), "private", "secrets")}
	value := "spaces 'quotes' $dollars `ticks`\\slashes\nsecond line\n日本語"
	if err := s.Set("vpn_password", value); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("vpn_password")
	if err != nil || got != value {
		t.Fatalf("round trip: %q %v", got, err)
	}
	info, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %o", info.Mode().Perm())
	}
	data, _ := os.ReadFile(s.path)
	if strings.Contains(string(data), value) {
		t.Fatal("file format unexpectedly contains literal value")
	}
	if err := s.Delete("vpn_password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("vpn_password"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("deleted key: %v", err)
	}
}

func TestConcurrentFileWrites(t *testing.T) {
	s := fileStore{filepath.Join(t.TempDir(), "secrets")}
	var wg sync.WaitGroup
	for _, name := range []string{"user", "vpn_password", "totp_secret", "hpc_user", "hpc_password", "hpc_totp_secret", "storage_check"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := s.Set(name, name); err != nil {
				t.Error(err)
			}
		}(name)
	}
	wg.Wait()
	for _, name := range []string{"user", "vpn_password", "totp_secret", "hpc_user", "hpc_password", "hpc_totp_secret", "storage_check"} {
		got, err := s.Get(name)
		if err != nil || got != name {
			t.Errorf("lost concurrent key %s: %q %v", name, got, err)
		}
	}
}

func TestCorruptFilePreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets")
	original := "vpn_password=%%%\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (fileStore{path}).Set("user", "test"); err == nil {
		t.Fatal("overwrote corrupt file")
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("corrupt file was changed")
	}
}

func TestEncryptedStore(t *testing.T) {
	keyring.MockInit()
	s := encryptedStore{}
	if err := s.Set("vpn_password", "secret"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("vpn_password")
	if err != nil || got != "secret" {
		t.Fatalf("encrypted round trip: %q %v", got, err)
	}
	if err := s.Delete("vpn_password"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("absent"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("absent"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestEncryptedFailureGivesUnlockInstructions(t *testing.T) {
	keyring.MockInitWithError(errors.New("collection is locked"))
	defer keyring.MockInit()
	_, err := (encryptedStore{}).Get("user")
	if err == nil || !strings.Contains(err.Error(), "secrets --unlock") {
		t.Fatalf("missing unlock instructions: %v", err)
	}
	if errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("backend failure treated as missing credential")
	}
}

func TestKeyringTimeout(t *testing.T) {
	old := keyringTimeout
	keyringTimeout = 10 * time.Millisecond
	defer func() { keyringTimeout = old }()
	release := make(chan struct{})
	defer close(release)
	_, err := keyringCall(func() (string, error) { <-release; return "", nil })
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("no timeout guidance: %v", err)
	}
}
