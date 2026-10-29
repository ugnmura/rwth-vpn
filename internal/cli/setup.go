package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ugnmura/rwth-vpn/internal/prompt"
	"github.com/ugnmura/rwth-vpn/internal/setup"
)

func terminalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func runSetup(args []string) int {
	fs, opts := commandFlags(command{name: "secrets"}, os.Stderr)
	if err := fs.Parse(args); err != nil {
		return report(err, 2)
	}
	if opts.where || opts.unlock {
		forwarded := []string{"secrets"}
		if opts.mode != "" {
			forwarded = append(forwarded, "--mode", opts.mode)
		}
		if opts.unlock {
			forwarded = append(forwarded, "--unlock")
		} else {
			forwarded = append(forwarded, "--where")
		}
		return runScript(forwarded)
	}
	if !prompt.IsTerminal(os.Stdin) {
		return report(errors.New("run credential setup in a terminal"), 1)
	}
	ctx, cancel := terminalContext()
	defer cancel()
	terminal := prompt.New(ctx, os.Stdin, os.Stdout, opts.plain)
	defer terminal.Close()
	home, err := os.UserHomeDir()
	if err != nil {
		return report(err, 1)
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	options := setup.Options{Mode: opts.mode}
	if opts.hpc {
		hpc := true
		options.HPC = &hpc
	} else if opts.noHPC {
		hpc := false
		options.HPC = &hpc
	}
	result, err := setup.Run(terminal, setup.Paths{Dir: filepath.Join(base, "rwth-vpn")}, options)
	if prompt.IsCanceled(err) {
		fmt.Fprintln(os.Stdout, "Canceled; existing credentials and settings kept.")
		return 0
	}
	if err != nil {
		return report(err, 1)
	}
	if !result.Saved {
		fmt.Fprintln(os.Stdout, "Canceled; existing credentials and settings kept.")
		return 0
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = filepath.Join(home, ".cache")
	}
	for _, name := range []string{"vpn-login-failed", "hpc-login-failed"} {
		_ = os.Remove(filepath.Join(runtimeDir, "rwth-vpn", name))
	}
	if result.HPC {
		terminal.Close()
		return runScript([]string{"ssh-setup"})
	}
	return 0
}

func interactiveMenu(version string) int {
	ctx, cancel := terminalContext()
	defer cancel()
	terminal := prompt.New(ctx, os.Stdin, os.Stdout, false)
	defer terminal.Close()
	labels := []string{"VPN status", "Connect VPN", "Disconnect VPN", "Credential setup", "HPC setup", "Help", "Exit"}
	actions := [][]string{{"status"}, {"up"}, {"down"}, {"secrets"}, {"secrets", "--hpc"}, {"--help"}, nil}
	selected, err := terminal.Choose("rwth-vpn", labels, 0)
	if prompt.IsCanceled(err) {
		return 0
	}
	if err != nil {
		return report(err, 1)
	}
	if selected == len(actions)-1 {
		return 0
	}
	// Restore terminal state before a command replaces this process with Bash.
	terminal.Close()
	return Run(actions[selected], version)
}
