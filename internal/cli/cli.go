// Package cli dispatches commands and starts the embedded VPN orchestration.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/ugnmura/rwth-vpn/internal/credentials"
	"github.com/ugnmura/rwth-vpn/internal/prompt"
	runtimebundle "github.com/ugnmura/rwth-vpn/internal/runtime"
)

// Run executes a command and returns its process exit status.
func Run(args []string, version string) int {
	if len(args) > 0 && args[0] == "__native" {
		if err := native(args[1:]); err != nil {
			if errors.Is(err, credentials.ErrNotFound) {
				return 1
			}
			return report(err, 3)
		}
		return 0
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "_") {
		return runScript(args)
	}
	// OpenSSH invokes this executable with a prompt instead of a command.
	if os.Getenv("RWTH_VPN_ASKPASS") != "" && len(args) == 1 && !strings.HasPrefix(args[0], "-") && args[0] != "help" {
		if _, ok := findCommand(args[0]); !ok {
			return runScript(args)
		}
	}
	root, showVersion := rootFlags(os.Stderr)
	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Println("rwth-vpn", version)
		return 0
	}
	args = root.Args()
	if len(args) == 0 {
		if prompt.IsTerminal(os.Stdin) && prompt.IsTerminal(os.Stdout) {
			return interactiveMenu(version)
		}
		return help(nil)
	}
	if args[0] == "help" {
		return help(args[1:])
	}
	cmd, ok := findCommand(args[0])
	if !ok {
		return report(fmt.Errorf("unknown command %q; run rwth-vpn --help", args[0]), 2)
	}
	normalized, err := parseCommand(cmd, args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return report(err, 2)
	}
	if cmd.name == "configure" {
		return configure(normalized[1:])
	}
	if cmd.name == "secrets" {
		return runSetup(normalized[1:])
	}
	return runScript(normalized)
}

func help(args []string) int {
	fs := flag.NewFlagSet("help", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprintln(fs.Output(), "Usage: rwth-vpn help [command|topic]") }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if err := printHelp(os.Stdout, fs.Args()); err != nil {
		return report(err, 2)
	}
	return 0
}

func runScript(args []string) int {
	self, err := os.Executable()
	if err != nil {
		return report(err, 1)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		return report(errors.New("bash is required; install it with your package manager"), 1)
	}
	// Replacing the process preserves signals, VPN hooks and SSH_ASKPASS behavior.
	// The embedded script is never extracted to disk.
	return report(syscall.Exec(bash, append([]string{"bash", "-c", runtimebundle.Script, self}, args...), os.Environ()), 1)
}

func report(err error, status int) int {
	fmt.Fprintln(os.Stderr, "rwth-vpn:", err)
	return status
}
