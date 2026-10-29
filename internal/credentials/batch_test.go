package credentials

import (
	"errors"
	"path/filepath"
	"testing"
)

type failingStore struct {
	values map[string]string
	fail   bool
}

func (s *failingStore) Get(k string) (string, error) {
	v, ok := s.values[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (s *failingStore) Set(k, v string) error {
	if k == "b" && s.fail {
		s.fail = false
		return errors.New("test write failed")
	}
	s.values[k] = v
	return nil
}
func (s *failingStore) Delete(k string) error { delete(s.values, k); return nil }
func TestBatchRollsBackKeyringFailure(t *testing.T) {
	store := &failingStore{values: map[string]string{"a": "original-a", "b": "original-b"}, fail: true}
	a, b := "new-a", "new-b"
	if err := Apply(store, map[string]*string{"a": &a, "b": &b}); err == nil {
		t.Fatal("accepted failing batch")
	}
	if store.values["a"] != "original-a" || store.values["b"] != "original-b" {
		t.Fatal("failed batch partially overwrote credentials")
	}
}
func TestFileBatchPreservesOtherKeys(t *testing.T) {
	store := fileStore{filepath.Join(t.TempDir(), "secrets")}
	if err := store.Set("unrelated", "keep"); err != nil {
		t.Fatal(err)
	}
	password := "new-password"
	if err := Apply(store, map[string]*string{"vpn_password": &password, "user": nil}); err != nil {
		t.Fatal(err)
	}
	if value, _ := store.Get("unrelated"); value != "keep" {
		t.Fatal("batch removed unrelated keys")
	}
}
