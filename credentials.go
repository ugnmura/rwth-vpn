package main

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

var secretNames = []string{"user", "vpn_password", "totp_secret", "hpc_user", "hpc_password", "hpc_totp_secret", "storage_check"}

type credentialStore interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
}

func newStore(backend, path string) (credentialStore, error) {
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

type encryptedStore struct{}

var keyringTimeout = 20 * time.Second

// A blocked GUI prompt must not hang unattended SSH forever. Returning from
// the helper terminates its outstanding keyring goroutine too.
func keyringCall[T any](fn func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	ch := make(chan result, 1)
	go func() { v, err := fn(); ch <- result{v, err} }()
	select {
	case r := <-ch:
		if r.err != nil && !errors.Is(r.err, keyring.ErrNotFound) {
			return r.value, fmt.Errorf("encrypted storage unavailable: %w; unlock with 'rwth-vpn secrets --unlock' (Linux needs a session D-Bus and Secret Service provider)", r.err)
		}
		return r.value, r.err
	case <-time.After(keyringTimeout):
		var zero T
		return zero, errors.New("keyring timed out; run 'rwth-vpn secrets --unlock' in an interactive session; no unencrypted fallback was used")
	}
}
func (encryptedStore) Get(name string) (string, error) {
	return keyringCall(func() (string, error) { return keyring.Get("rwth-vpn", name) })
}
func (encryptedStore) Set(name, value string) error {
	_, err := keyringCall(func() (bool, error) { return true, keyring.Set("rwth-vpn", name, value) })
	return err
}
func (encryptedStore) Delete(name string) error {
	_, err := keyringCall(func() (bool, error) {
		err := keyring.Delete("rwth-vpn", name)
		if errors.Is(err, keyring.ErrNotFound) {
			err = nil
		}
		return true, err
	})
	return err
}

func readSecret(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1024*1024+1))
	if len(data) > 1024*1024 {
		return "", errors.New("credential exceeds 1 MiB")
	}
	return string(data), err
}

type fileStore struct{ path string }

func (s fileStore) read() (map[string]string, error) {
	values := map[string]string{}
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 2*1024*1024)
	for scan.Scan() {
		key, value, ok := strings.Cut(scan.Text(), "=")
		if !ok || key == "" {
			return nil, errors.New("invalid credentials file")
		}
		data, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, errors.New("invalid base64 in credentials file")
		}
		values[key] = string(data)
	}
	return values, scan.Err()
}
func (s fileStore) Get(name string) (string, error) {
	values, err := s.read()
	if err != nil {
		return "", err
	}
	value, ok := values[name]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}
func (s fileStore) Set(name, value string) error { return s.update(name, value, false) }
func (s fileStore) Delete(name string) error     { return s.update(name, "", true) }
func (s fileStore) update(name, value string, remove bool) error {
	if name == "" || strings.ContainsAny(name, "=\r\n") {
		return errors.New("invalid credential name")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	// Serialize read-modify-write so concurrent setup operations do not lose keys.
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	values, err := s.read()
	if err != nil {
		return err
	}
	if remove {
		delete(values, name)
	} else {
		values[name] = value
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".secrets-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := fmt.Fprintf(tmp, "%s=%s\n", k, base64.StdEncoding.EncodeToString([]byte(values[k]))); err != nil {
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
