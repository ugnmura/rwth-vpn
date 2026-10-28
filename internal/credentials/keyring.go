package credentials

import (
	"errors"
	"fmt"
	"time"

	"github.com/zalando/go-keyring"
)

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
	return keyringDo(func() error { return keyring.Set("rwth-vpn", name, value) })
}

func (encryptedStore) Delete(name string) error {
	return keyringDo(func() error {
		err := keyring.Delete("rwth-vpn", name)
		if errors.Is(err, keyring.ErrNotFound) {
			err = nil
		}
		return err
	})
}

func keyringDo(fn func() error) error {
	_, err := keyringCall(func() (struct{}, error) { return struct{}{}, fn() })
	return err
}
