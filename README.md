# rwth-vpn

> [!WARNING]
> This is a personal tool, tested against RWTH's real VPN and CLAIX. RWTH can change things on their end at any time. Keep your expectations reasonable.

Connect to the RWTH Aachen VPN and the CLAIX HPC cluster from Linux, macOS or BSD without the Cisco client. You store your secrets once, and after that there are no passwords or TOTP codes to type. It plays nicely with Tailscale.

## Quick Start

```console
$ git clone git@github.com:ugnmura/rwth-vpn.git ~/src/rwth-vpn
$ ~/src/rwth-vpn/install.sh
$ ssh claix
```

That's it. The installer never uses sudo. If a system package is missing, it tells you the command to install it and stops, so you can run that command and rerun the installer.

## Usage

```console
$ ssh claix                # HPC login, no sudo, no prompts
$ rwth-vpn up              # route the whole machine through RWTH (asks for sudo)
$ rwth-vpn up --split      # only RWTH networks through the VPN
$ rwth-vpn status
$ rwth-vpn down
```

### `ssh claix`

You don't need to run `rwth-vpn up` first. If the VPN isn't up, ssh starts a small VPN session just for itself, logs in with your stored password and TOTP, and keeps the connection open for 8 hours. Later `ssh`, `scp` and `rsync` calls reuse it instantly.

`claix`, `login23-*` and `copy23-*` all work, and so does VS Code Remote-SSH. Agents can use it too: `ssh -o BatchMode=yes claix 'sbatch job.sh'` works fine.

### `rwth-vpn up`

Use this when you need more than ssh, for example to download papers from IEEE, Springer or Elsevier. Those sites check that your traffic comes from RWTH.

- **Full tunnel** (the default): all your traffic goes through RWTH. Tailscale keeps working directly, so you don't lose your ssh session to the machine.
- **Split tunnel** (`--split`): only RWTH addresses go through the VPN. That's enough for CLAIX, but publishers won't see you as RWTH.

To make split the default, put `RWTH_MODE=split` in `~/.config/rwth-vpn/config`.

## Secrets

`rwth-vpn secrets` asks for everything and shows where to get each secret. You only need the HPC secrets if you use the HPC.

| Secret | Where to get it |
|---|---|
| RWTH username | Your TIM ID, e.g. `ab123456` |
| VPN password | [IdM Selfservice](https://idm.rwth-aachen.de/selfservice/). Not your SSO password. |
| VPN TOTP secret | [IdM Selfservice](https://idm.rwth-aachen.de/selfservice/) → Token Manager → new *Authenticator app (TOTP)* token |
| HPC password | [RegApp](https://regapp.itc.rwth-aachen.de/) |
| HPC TOTP secret | [RegApp](https://regapp.itc.rwth-aachen.de/), the HPC's own second factor |

When you create a TOTP token, copy the secret text (`JBSWY3DP…`) or the `otpauth://` link. Scan the same QR code with your phone too, so you can still log in by hand. `rwth-vpn totp` (or `rwth-vpn totp hpc`) prints the current code, which should match your phone.

On macOS the secrets are stored in the Keychain. Everywhere else they go in `~/.config/rwth-vpn/secrets` (mode 0600).

**Be aware:** this machine now holds both factors. Anyone with your user account on it gets your RWTH VPN and HPC access.

## Supported Platforms

- Debian, Ubuntu, Fedora: everything is packaged
- Arch, Manjaro, openSUSE, Void, Alpine, Gentoo, Solus: `ocproxy` is built from source
- NixOS: add `openconnect` and `ocproxy` to your config
- macOS: via Homebrew
- FreeBSD, OpenBSD, NetBSD: full tunnel only

## Why Not Just Use openconnect?

Plain openconnect doesn't work with RWTH, for two reasons:

1. RWTH's Cisco ASA answers **404** to any client that doesn't call itself `AnyConnect`. `rwth-vpn` sends the right User-Agent.
2. openconnect **older than 9.20** gets "Cookie was rejected" after logging in, because of a TLS 1.3 bug ([fixed upstream](https://gitlab.com/openconnect/openconnect/-/commit/94e0b16c)). Many distros still ship 9.12. `rwth-vpn` limits those versions to TLS 1.2 without changing anything system-wide.

If logins suddenly break, run `rwth-vpn check` first.

## Known Issues

- RWTH's VPN uses `172.21.0.0/16`. If one of your Docker networks uses that range, `rwth-vpn up` clashes with it. `ssh claix` is not affected.
- The HPC login prompts are matched by their text. If RWTH rewords them, the login stops instead of guessing, and `~/.cache/rwth-vpn/ssh.log` shows the new prompt.
- On macOS and the BSDs, the full tunnel doesn't block IPv6, so publishers might still see your home IPv6 address.
- In full mode, if DHCP rewrites `/etc/resolv.conf`, RWTH-internal names stop resolving until you reconnect.
