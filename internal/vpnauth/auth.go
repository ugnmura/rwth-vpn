// Package vpnauth submits VPN credentials directly to openconnect over stdin.
package vpnauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/ugnmura/rwth-vpn/internal/credentials"
	"github.com/ugnmura/rwth-vpn/internal/totp"
)

// Authenticate overlaps the TLS handshake with waiting for a fresh TOTP code.
// It makes one attempt and never exposes credentials in arguments or environment.
func Authenticate(ctx context.Context, store credentials.Store, args []string, stdout, stderr io.Writer) error {
	password, err := store.Get("vpn_password")
	if err != nil {
		return err
	}
	secret, err := store.Get("totp_secret")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "openconnect", args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return run(ctx, cmd, password, secret, func(ctx context.Context, secret string) (string, error) {
		return totp.GenerateContext(ctx, secret, true)
	})
}

type codeGenerator func(context.Context, string) (string, error)

func run(ctx context.Context, cmd *exec.Cmd, password, secret string, generate codeGenerator) error {
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	if err := cmd.Start(); err != nil {
		return err
	}
	tokenCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { err := cmd.Wait(); cancel(); done <- err }()
	// Start the network process first. A rejected/closed connection cancels the
	// token wait instead of burning another 30-second slot after the attempt ends.
	_, inputErr := io.WriteString(input, password+"\n")
	if inputErr == nil {
		var code string
		code, inputErr = generate(tokenCtx, secret)
		if inputErr == nil {
			_, inputErr = io.WriteString(input, code+"\n")
		}
	}
	input.Close()
	if inputErr != nil {
		_ = cmd.Process.Kill()
	}
	processErr := <-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if inputErr != nil && !errors.Is(inputErr, context.Canceled) {
		return fmt.Errorf("submitting VPN credentials: %w", inputErr)
	}
	if processErr != nil {
		return fmt.Errorf("openconnect authentication: %w", processErr)
	}
	return nil
}
