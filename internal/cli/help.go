package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
)

// Keep flag's parser and option metadata, but display conventional long names.
func printOptions(fs *flag.FlagSet, out io.Writer) {
	fmt.Fprintln(out, "\nOptions:")
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "  -h, --help\tShow help")
	fs.VisitAll(func(option *flag.Flag) {
		value, description := flag.UnquoteUsage(option)
		name := "--" + option.Name
		if value != "" {
			name += " " + value
		}
		fmt.Fprintf(table, "  %s\t%s\n", name, description)
	})
	_ = table.Flush()
}

var topics = map[string]string{
	"configure": `Read a setup profile from a private JSON file or stdin. The default is a
redacted preview: no credentials or settings are changed. --apply saves only
after the human has approved that summary. --json returns names/actions only.

Schema:
  {"mode":"encrypted|unencrypted","hpc":true|false,"credentials":{...}}
Credential names: user, vpn_password, totp_secret, hpc_user, hpc_password,
hpc_totp_secret. Omitted values are kept; null clears a value. TOTP fields need
a base32 secret or otpauth:// URI, not the current six-digit code.

Never put passwords or TOTP secrets in command arguments, environment variables,
chat, or recorded tool-call bodies. A secure UI can supply a private 0600 file
or a protected stdin stream. See docs/AGENTS.md in the source repository.
After applying an HPC profile, run rwth-vpn ssh-setup to configure SSH aliases.
`,
	"up": `Full tunnel is the default: all traffic goes through RWTH, including publisher
websites. Tailscale keeps its direct route. --split routes only RWTH networks;
publishers will still see your normal IP address.

Requires Bash, openconnect, the system vpnc-script, and iproute2 on Linux.
--split also needs vpn-slice. Authentication runs as your user; only the tunnel
uses sudo/doas. Store credentials with rwth-vpn secrets first.

Stop with rwth-vpn down. Set RWTH_MODE=split in the config to default to split.

TLS-only transport is the default. Some IPv6/NAT paths establish DTLS but pass
no tunnel traffic. --dtls opts into UDP transport; --no-dtls forces TLS-only.
If the VPN process exits unexpectedly, down restores its saved routing/DNS.
`,
	"secrets": `Setup offers a storage mode and VPN credentials. HPC setup is optional.
--no-hpc skips HPC prompts and SSH setup; --hpc enables them later.
Enter keeps a value; '-' stages its removal. --where explains where to get credentials.
Edits stay in memory until the final Save changes confirmation. Cancel, Ctrl-C,
or closing input keeps credentials, storage mode and HPC preferences unchanged.
Use arrow keys to choose storage; limited terminals use numbered choices.
--plain forces numbered prompts. No need to type the storage-mode name.

Encrypted storage uses macOS login Keychain or Linux Secret Service (service
rwth-vpn). Linux needs a running Secret Service provider, such as GNOME Keyring,
and a session D-Bus. secret-tool is not required. Protect the keyring with a
password; the app does not save its unlock password or fall back to a file.

Use --unlock in a terminal when locked, including over SSH. Terminal unlocking
supports Keychain and GNOME Keyring. Other providers need their own unlock flow.
SSH public-key login does not unlock a keyring. Headless sessions must reach the
same session bus/keyring. Before selecting encrypted mode for the first time:
  rwth-vpn secrets --mode encrypted --unlock

Unencrypted storage uses $XDG_CONFIG_HOME/rwth-vpn/secrets, defaulting to
~/.config/rwth-vpn/secrets (0600, base64 encoded, NOT encrypted).
Switching modes copies and verifies credentials before removing the originals.
Existing installations keep their backend until you choose a mode.

This machine holds passwords AND TOTP secrets. Access to your user account can
give access to both factors. Keep a copy of each TOTP token on your phone too.
`,
	"ssh": `Install optional HPC dependencies with sh install.sh --hpc. Configure credentials
with rwth-vpn secrets --hpc. VPN and HPC use separate passwords and TOTP tokens.
Use rwth-vpn secrets --where for the IdM and RegApp instructions.

ssh-setup writes ~/.ssh/rwth-vpn.conf and adds its Include to ~/.ssh/config.
claix, login23-* and copy23-* use the VPN on demand; no sudo is needed.
OpenSSH and ocproxy are required. Connections persist for 8 hours and are reused
by ssh, scp, rsync and VS Code Remote-SSH. BatchMode works with stored credentials.

After a rejected login, automatic retries pause for 15 minutes to avoid account
lockout. Correct credentials with rwth-vpn secrets to resume.
`,
	"uninstall": `Stops managed SSH sessions and the rootless proxy, removes SSH integration and
the installed binary. Credentials and settings are kept for reinstalling.
--purge also deletes credentials from the active keyring/file backend and settings.
Unlock encrypted storage first if necessary. Stop a whole-machine tunnel with
rwth-vpn down before uninstalling. Dependencies and diagnostic caches are kept.
`,
	"config": `Config: $XDG_CONFIG_HOME/rwth-vpn/config (default ~/.config/rwth-vpn/config)

Optional shell assignments:
  RWTH_USER=ab123456       VPN username (otherwise read from stored credentials)
  RWTH_HPC_USER=ab123456   HPC username override
  RWTH_MODE=full          Default tunnel mode: full or split
  RWTH_DTLS=0             TLS-only by default; 1 opts into UDP/DTLS transport
  RWTH_SOCKS_PORT=11080   Rootless SSH proxy port

Only put trusted shell code in this file. Use rwth-vpn secrets for passwords and
TOTP secrets. Storage mode and HPC setup preferences are saved separately.

Rootless logs: $XDG_RUNTIME_DIR/rwth-vpn, or ~/.cache/rwth-vpn when unset.
Whole-machine log: /var/log/rwth-vpn.log.
`,
	"install": `Download https://github.com/ugnmura/rwth-vpn/releases/latest/download/install.sh
and run sh install.sh. The installer downloads a binary for your OS/architecture,
verifies its SHA-256 checksum and installs to ~/.local/bin (Homebrew bin on macOS).
It never uses sudo; missing system dependencies are listed for you to install.
Add the destination to PATH if warned. Run the installer again to update.

Options: --hpc, --no-hpc, --with-split, --no-split, --user ab123456.
VPN-only is the default. --hpc adds OpenSSH/ocproxy; --with-split optionally
installs vpn-slice, which has its own Python requirements. The binary itself
requires neither Python nor a Go compiler.

Each release installer selects that version. RWTH_VPN_VERSION overrides it.
From a source checkout, make build first; install.sh uses dist/rwth-vpn if present.
Release targets: Linux, macOS, FreeBSD, OpenBSD, NetBSD; amd64 and arm64.
Encrypted storage supports Linux/macOS. Native validation has run on Linux.
`,
	"troubleshooting": `Start with rwth-vpn check; use rwth-vpn test-login to test stored credentials.

Locked/unavailable keyring: rwth-vpn secrets --unlock. Headless Linux needs a
running session D-Bus and Secret Service provider. See rwth-vpn help secrets.

Rejected logins: check ~/.cache/rwth-vpn/ssh.log and proxy.log (or the
XDG_RUNTIME_DIR/rwth-vpn equivalents). Automatic retries pause for 15 minutes;
rwth-vpn secrets corrects credentials and clears the pause. HPC prompt wording
changes may require a tool update.

Docker networks using 172.21.0.0/16 can clash with RWTH's whole-machine tunnel.
The rootless SSH session is unaffected.

On macOS/BSD, full tunnel does not block native IPv6; publishers may see your
home IPv6 address. If DHCP rewrites /etc/resolv.conf during a full tunnel,
reconnect to restore RWTH DNS.

RWTH requires an AnyConnect User-Agent. This tool also limits older openconnect
versions to TLS 1.2 to work around rejected authentication cookies.
`,
	"status":     "Shows rootless proxy and whole-machine tunnel status. Returns 1 without a\nwhole-machine tunnel, even if the rootless SSH proxy is running.\n",
	"down":       "Stops the rootless proxy and whole-machine VPN. A root-owned whole-machine\ntunnel needs sudo/doas.\n",
	"check":      "Checks reachability and the VPN login form without submitting credentials.\n",
	"test-login": "Authenticates with stored VPN credentials without starting a tunnel.\n",
	"totp":       "Prints the current code for the stored VPN token, or the separate HPC token.\nSet up tokens with rwth-vpn secrets; see rwth-vpn secrets --where.\n",
}

func printOverview(out io.Writer) {
	fmt.Fprintln(out, "rwth-vpn — RWTH VPN and optional CLAIX access\n\nUsage: rwth-vpn [options] COMMAND [arguments]\n\nCommands:")
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for _, cmd := range commands {
		label := "rwth-vpn " + cmd.name
		if cmd.positional != "" {
			label += " " + cmd.positional
		}
		fmt.Fprintf(table, "  %s\t%s\n", label, cmd.summary)
	}
	_ = table.Flush()
	fmt.Fprintln(out, "\nUse rwth-vpn COMMAND --help or rwth-vpn help COMMAND for details.\nAdditional help: config, install, troubleshooting.")
}

func printHelp(out io.Writer, args []string) error {
	if len(args) == 0 {
		fs, _ := rootFlags(out)
		fs.Usage()
		return nil
	}
	if len(args) != 1 {
		return errors.New("usage: rwth-vpn help [command|topic]")
	}
	topic := args[0]
	if topic == "ssh" {
		topic = "ssh-setup"
	}
	if cmd, ok := findCommand(topic); ok {
		fs, _ := commandFlags(cmd, out)
		fs.Usage()
		return nil
	}
	if text, ok := topics[topic]; ok {
		fmt.Fprint(out, text)
		return nil
	}
	return fmt.Errorf("unknown help topic %q; run rwth-vpn --help", args[0])
}
