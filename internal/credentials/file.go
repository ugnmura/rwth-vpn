package credentials

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ugnmura/rwth-vpn/internal/filelock"
)

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
		return "", ErrNotFound
	}
	return value, nil
}

func (s fileStore) Set(name, value string) error { return s.update(name, value, false) }
func (s fileStore) Delete(name string) error     { return s.update(name, "", true) }
func (s fileStore) update(name, value string, remove bool) error {
	changes := map[string]*string{name: &value}
	if remove {
		changes[name] = nil
	}
	return s.apply(changes)
}

func (s fileStore) apply(changes map[string]*string) error {
	for name := range changes {
		if name == "" || strings.ContainsAny(name, "=\r\n") {
			return errors.New("invalid credential name")
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	// Serialize read-modify-write so concurrent setup operations do not lose keys.
	lock, err := filelock.Acquire(s.path + ".lock")
	if err != nil {
		return err
	}
	defer filelock.Release(lock)
	values, err := s.read()
	if err != nil {
		return err
	}
	for name, value := range changes {
		if value == nil {
			delete(values, name)
		} else {
			values[name] = *value
		}
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
