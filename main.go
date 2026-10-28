package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/zalando/go-keyring"
)

//go:embed internal/runtime/rwth-vpn.sh
var script string

var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println("rwth-vpn", version)
		return
	}
	if len(args) > 0 && args[0] == "__native" {
		if err := native(args[1:]); err != nil {
			if errors.Is(err, keyring.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "rwth-vpn:", err)
			os.Exit(3)
		}
		return
	}
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		fatal(errors.New("bash is required; install it with your package manager"))
	}
	// Replace this process so signal handling, VPN hooks and SSH_ASKPASS retain
	// the same behavior as the original script. No script is extracted to disk.
	if err := syscall.Exec(bash, append([]string{"bash", "-c", script, self}, args...), os.Environ()); err != nil {
		fatal(err)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "rwth-vpn:", err); os.Exit(1) }

func native(args []string) error {
	if len(args) == 0 {
		return errors.New("missing native operation")
	}
	switch args[0] {
	case "help":
		for _, line := range strings.Split(script, "\n")[1:] {
			if !strings.HasPrefix(line, "#") {
				break
			}
			fmt.Println(strings.TrimPrefix(strings.TrimPrefix(line, "#"), " "))
		}
		return nil
	case "totp":
		return printTOTP(os.Stdin, os.Stdout, len(args) > 1 && args[1] == "--fresh")
	case "connect":
		if len(args) < 3 || len(args) > 4 {
			return errors.New("connect requires host, port, and optional SOCKS port")
		}
		proxy := ""
		if len(args) == 4 {
			proxy = args[3]
		}
		return connect(args[1], args[2], proxy, os.Stdin, os.Stdout)
	case "port-open":
		if len(args) != 2 {
			return errors.New("port-open requires a port")
		}
		return portOpen(args[1])
	case "gateway":
		if len(args) != 2 {
			return errors.New("gateway requires a host")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		addr, err := gateway(ctx, args[1])
		if err == nil {
			fmt.Println(addr)
		}
		return err
	case "secret-get", "secret-set":
		if len(args) != 4 {
			return errors.New("credential operation requires backend, file, and account")
		}
		store, err := newStore(args[1], args[2])
		if err != nil {
			return err
		}
		if args[0] == "secret-get" {
			value, err := store.Get(args[3])
			if err == nil {
				fmt.Print(value)
			}
			return err
		}
		value, err := readSecret(os.Stdin)
		if err != nil {
			return err
		}
		if value == "" {
			return store.Delete(args[3])
		}
		return store.Set(args[3], value)
	case "secret-purge":
		if len(args) != 2 {
			return errors.New("secret-purge requires backend")
		}
		store, err := newStore(args[1], "")
		if err != nil {
			return err
		}
		if _, ok := store.(encryptedStore); !ok {
			return errors.New("keyring purge requires an encrypted backend")
		}
		_, err = keyringCall(func() (bool, error) { return true, keyring.DeleteAll("rwth-vpn") })
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	default:
		return fmt.Errorf("unknown native operation %q", args[0])
	}
}
