package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCanceledLockWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	held, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer Release(held)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if f, err := AcquireContext(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if f != nil {
			Release(f)
		}
		t.Fatalf("lock wait did not cancel: %v", err)
	}
}
