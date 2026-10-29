package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ugnmura/rwth-vpn/internal/credentials"
	"github.com/ugnmura/rwth-vpn/internal/network"
	"github.com/ugnmura/rwth-vpn/internal/sshstate"
	"github.com/ugnmura/rwth-vpn/internal/totp"
	"github.com/ugnmura/rwth-vpn/internal/vpnauth"
)

func native(args []string) error {
	if len(args) == 0 {
		return errors.New("missing native operation")
	}
	switch args[0] {
	case "ssh-session":
		if len(args) != 6 {
			return errors.New("ssh-session requires action, marker, and transport metadata paths")
		}
		session := sshstate.Current(args[3], args[4], args[5])
		switch args[1] {
		case "check":
			ok, err := sshstate.Matches(args[2], session)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("SSH master belongs to an earlier VPN session")
			}
			return nil
		case "record":
			return sshstate.Record(args[2], session)
		default:
			return errors.New("unknown SSH session action")
		}

	case "authenticate":
		if len(args) < 4 {
			return errors.New("authenticate requires backend, file, and openconnect arguments")
		}
		store, err := credentials.New(args[1], args[2])
		if err != nil {
			return err
		}
		path := os.Getenv("AUTH_LOG")
		if path == "" {
			path = os.DevNull
		}
		log, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		defer log.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return vpnauth.Authenticate(ctx, store, args[3:], os.Stdout, log)
	case "help":
		return printHelp(os.Stdout, args[1:])
	case "totp":
		raw, err := readSecret(os.Stdin)
		if err != nil {
			return err
		}
		code, err := totp.Generate(raw, len(args) > 1 && args[1] == "--fresh")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, code)
		return err
	case "connect":
		if len(args) < 3 || len(args) > 4 {
			return errors.New("connect requires host, port, and optional SOCKS port")
		}
		proxy := ""
		if len(args) == 4 {
			proxy = args[3]
		}
		return network.Connect(args[1], args[2], proxy, os.Stdin, os.Stdout)
	case "port-open":
		if len(args) != 2 {
			return errors.New("port-open requires a port")
		}
		return network.PortOpen(args[1])
	case "gateway":
		if len(args) != 2 {
			return errors.New("gateway requires a host")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		addr, err := network.Gateway(ctx, args[1])
		if err == nil {
			fmt.Println(addr)
		}
		return err
	case "secret-get", "secret-set":
		if len(args) != 4 {
			return errors.New("credential operation requires backend, file, and account")
		}
		store, err := credentials.New(args[1], args[2])
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
		return credentials.Purge(args[1])
	default:
		return fmt.Errorf("unknown native operation %q", args[0])
	}
}
