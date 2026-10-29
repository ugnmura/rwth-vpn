package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
)

type command struct {
	name       string
	summary    string
	positional string
}

var commands = []command{
	{"configure", "Preview or apply an agent-provided setup profile", ""},
	{"secrets", "Store credentials and choose encrypted or unencrypted storage", ""},
	{"up", "Connect the whole machine through RWTH", ""},
	{"down", "Stop VPN sessions", ""},
	{"status", "Show connection status", ""},
	{"check", "Check the VPN server without logging in", ""},
	{"test-login", "Test stored VPN credentials without starting a tunnel", ""},
	{"totp", "Print the current VPN or HPC TOTP code", "[hpc]"},
	{"ssh-setup", "Configure optional CLAIX access via ssh/scp/rsync", ""},
	{"uninstall", "Remove the program and SSH integration; keep credentials", ""},
}

func findCommand(name string) (command, bool) {
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd, true
		}
	}
	return command{}, false
}

type options struct {
	split, full                      bool
	dtls, noDTLS                     bool
	user                             string
	mode                             string
	hpc, noHPC, unlock, where, purge bool
	plain                            bool
	inputFile                        string
	stdin, apply, json               bool
}

func rootFlags(out io.Writer) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet("rwth-vpn", flag.ContinueOnError)
	fs.SetOutput(out)
	version := fs.Bool("version", false, "Print the build version")
	fs.Usage = func() { printOverview(out); printOptions(fs, out) }
	return fs, version
}

func commandFlags(cmd command, out io.Writer) (*flag.FlagSet, *options) {
	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(out)
	opts := new(options)
	switch cmd.name {
	case "configure":
		fs.StringVar(&opts.inputFile, "file", "", "Private setup JSON `path` (not printed)")
		fs.BoolVar(&opts.stdin, "stdin", false, "Read setup JSON from standard input")
		fs.BoolVar(&opts.apply, "apply", false, "Save the reviewed profile after explicit human approval")
		fs.BoolVar(&opts.json, "json", false, "Print a redacted machine-readable summary")
	case "up":
		fs.BoolVar(&opts.split, "split", false, "Route only RWTH networks (requires vpn-slice)")
		fs.BoolVar(&opts.full, "full", false, "Route all traffic through RWTH")
		fs.BoolVar(&opts.dtls, "dtls", false, "Enable UDP/DTLS transport (opt-in; some networks blackhole it)")
		fs.BoolVar(&opts.noDTLS, "no-dtls", false, "Use TLS-only transport (default)")
		fs.StringVar(&opts.user, "user", "", "RWTH `username`, e.g. ab123456")
	case "secrets":
		fs.BoolVar(&opts.plain, "plain", false, "Use numbered prompts instead of the interactive selector")
		fs.StringVar(&opts.mode, "mode", "", "Credential storage `mode`: encrypted or unencrypted")
		fs.BoolVar(&opts.hpc, "hpc", false, "Also set up HPC credentials and SSH access")
		fs.BoolVar(&opts.noHPC, "no-hpc", false, "Skip HPC credentials and SSH setup")
		fs.BoolVar(&opts.unlock, "unlock", false, "Unlock the keyring in this terminal, then exit")
		fs.BoolVar(&opts.where, "where", false, "Show where to get passwords and TOTP secrets")
	case "uninstall":
		fs.BoolVar(&opts.purge, "purge", false, "Also delete stored credentials and settings")
	}
	fs.Usage = func() {
		fmt.Fprintf(out, "Usage: rwth-vpn %s", cmd.name)
		hasOptions := false
		fs.VisitAll(func(*flag.Flag) { hasOptions = true })
		if hasOptions {
			fmt.Fprint(out, " [options]")
		}
		if cmd.positional != "" {
			fmt.Fprint(out, " ", cmd.positional)
		}
		fmt.Fprint(out, "\n\n", cmd.summary, ".\n")
		printOptions(fs, out)
		topic := cmd.name
		if topic == "ssh-setup" {
			topic = "ssh"
		}
		if text := topics[topic]; text != "" {
			fmt.Fprint(out, "\n", text)
		}
	}
	return fs, opts
}

// parseCommand validates user input and forwards a canonical argument list to
// the embedded orchestration. Internal SSH/VPN hooks bypass this public parser.
func parseCommand(cmd command, args []string, out io.Writer) ([]string, error) {
	var messages bytes.Buffer
	fs, opts := commandFlags(cmd, &messages)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, writeErr := io.Copy(out, &messages); writeErr != nil {
				return nil, writeErr
			}
		}
		return nil, err
	}
	provided := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { provided[f.Name] = true })
	if cmd.name == "totp" {
		if fs.NArg() > 1 || (fs.NArg() == 1 && fs.Arg(0) != "hpc") {
			return nil, errors.New("totp accepts only the optional argument 'hpc'")
		}
	} else if fs.NArg() != 0 {
		return nil, fmt.Errorf("%s does not accept positional arguments", cmd.name)
	}
	result := []string{cmd.name}
	switch cmd.name {
	case "up":
		if opts.dtls && opts.noDTLS {
			return nil, errors.New("choose either --dtls or --no-dtls")
		}
		if opts.dtls {
			result = append(result, "--dtls")
		}
		if opts.noDTLS {
			result = append(result, "--no-dtls")
		}
		if opts.split && opts.full {
			return nil, errors.New("choose either --split or --full")
		}
		if opts.split {
			result = append(result, "--split")
		}
		if opts.full {
			result = append(result, "--full")
		}
		if provided["user"] {
			result = append(result, "--user", opts.user)
		}
	case "configure":
		if opts.inputFile != "" && opts.stdin {
			return nil, errors.New("choose either --file or --stdin")
		}
		if opts.inputFile != "" {
			result = append(result, "--file", opts.inputFile)
		}
		if opts.stdin {
			result = append(result, "--stdin")
		}
		if opts.apply {
			result = append(result, "--apply")
		}
		if opts.json {
			result = append(result, "--json")
		}

	case "secrets":
		if opts.plain {
			result = append(result, "--plain")
		}
		if provided["mode"] && opts.mode != "encrypted" && opts.mode != "unencrypted" {
			return nil, errors.New("mode must be encrypted or unencrypted")
		}
		if opts.hpc && opts.noHPC {
			return nil, errors.New("choose either --hpc or --no-hpc")
		}
		if opts.mode != "" {
			result = append(result, "--mode", opts.mode)
		}
		if opts.unlock {
			result = append(result, "--unlock")
		}
		if opts.hpc {
			result = append(result, "--hpc")
		}
		if opts.noHPC {
			result = append(result, "--no-hpc")
		}
		if opts.where {
			result = append(result, "--where")
		}
	case "uninstall":
		if opts.purge {
			result = append(result, "--purge")
		}
	case "totp":
		result = append(result, fs.Args()...)
	}
	return result, nil
}
