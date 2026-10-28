// Package credentials provides file and operating-system keyring storage.
package credentials

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/zalando/go-keyring"
)

// ErrNotFound means an account is absent, rather than a failed backend.
var ErrNotFound = keyring.ErrNotFound

// Store reads, writes, and removes credentials by account name.
type Store interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
}

// New selects file, keychain, or secret-service storage. Path is used only by file storage.
func New(backend, path string) (Store, error) {
	switch backend {
	case "file":
		return fileStore{path}, nil
	case "keychain":
		if runtime.GOOS != "darwin" {
			return nil, errors.New("Keychain requires macOS")
		}
	case "secret-service":
		if runtime.GOOS != "linux" {
			return nil, errors.New("Secret Service requires Linux")
		}
	default:
		return nil, fmt.Errorf("unknown storage backend %q", backend)
	}
	return encryptedStore{}, nil
}

// Purge deletes this application's credentials from an encrypted backend.
func Purge(backend string) error {
	store, err := New(backend, "")
	if err != nil {
		return err
	}
	if _, ok := store.(encryptedStore); !ok {
		return errors.New("keyring purge requires an encrypted backend")
	}
	err = keyringDo(func() error { return keyring.DeleteAll("rwth-vpn") })
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
