package vpnauth

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func helperCommand(t *testing.T, ctx context.Context, mode, ready string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestAuthProcess$")
	cmd.Env = append(os.Environ(), "GO_AUTH_HELPER=1", "AUTH_MODE="+mode, "AUTH_READY="+ready)
	return cmd
}

func TestAuthProcess(t *testing.T) {
	if os.Getenv("GO_AUTH_HELPER") != "1" {
		return
	}
	ready := os.Getenv("AUTH_READY")
	if err := os.WriteFile(ready, []byte("ready"), 0600); err != nil {
		os.Exit(2)
	}
	input := bufio.NewScanner(os.Stdin)
	if !input.Scan() || input.Text() != "test-password" {
		os.Exit(3)
	}
	if os.Getenv("AUTH_MODE") == "reject" {
		for {
			if _, err := os.Stat(ready + ".waiting"); err == nil {
				os.Exit(9)
			}
			time.Sleep(time.Millisecond)
		}
	}
	if !input.Scan() || input.Text() != "123456" {
		os.Exit(4)
	}
	fmt.Println("COOKIE='test-cookie'")
	os.Exit(0)
}

func TestHandshakeStartsBeforeTokenWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := helperCommand(t, ctx, "success", ready)
	var output, diagnostics bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &diagnostics
	generate := func(ctx context.Context, secret string) (string, error) {
		if secret != "test-secret" {
			t.Error("wrong token passed to generator")
		}
		for {
			if _, err := os.Stat(ready); err == nil {
				return "123456", nil
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
	}
	if err := run(ctx, cmd, "test-password", "test-secret", generate); err != nil {
		t.Fatalf("authentication: %v %s", err, diagnostics.String())
	}
	if !strings.Contains(output.String(), "COOKIE='test-cookie'") {
		t.Fatalf("missing authentication result: %s", output.String())
	}
}

func TestRejectedConnectionCancelsUnusedTokenWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := helperCommand(t, ctx, "reject", ready)
	canceled := false
	generate := func(ctx context.Context, _ string) (string, error) {
		if err := os.WriteFile(ready+".waiting", nil, 0600); err != nil {
			return "", err
		}
		<-ctx.Done()
		canceled = true
		return "", ctx.Err()
	}
	err := run(ctx, cmd, "test-password", "test-secret", generate)
	if err == nil || !canceled || ctx.Err() != nil {
		t.Fatalf("did not stop token wait on process exit: canceled=%v err=%v", canceled, err)
	}
}
