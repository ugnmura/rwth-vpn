# rwth-vpn

> [!WARNING]
> This is a personal tool, tested against RWTH's real VPN and CLAIX. RWTH can change things on their end at any time. Keep your expectations reasonable.

Connect to the RWTH Aachen VPN and the CLAIX HPC cluster from Linux, macOS or BSD without the Cisco client. You store your secrets once, and after that there are no passwords or TOTP codes to type. It plays nicely with Tailscale.

## Quick Start

```console
$ curl -fsSL https://github.com/ugnmura/rwth-vpn/releases/latest/download/install.sh -o install.sh
$ sh install.sh
$ rwth-vpn up
```

Only `install.sh` is needed: it downloads the matching release binary over HTTPS, verifies its SHA-256 checksum, and installs it to `~/.local/bin` (Homebrew's bin on macOS). You can delete `install.sh` afterwards. To update, run the same commands again. With wget, use `wget -O install.sh https://github.com/ugnmura/rwth-vpn/releases/latest/download/install.sh` instead of curl. Each release installer defaults to its own version. To choose a specific release, replace `latest/download` in the URL with `download/v0.1.0`. You can also set `RWTH_VPN_VERSION` when running the installer.

The default is VPN-only. Add `--hpc` for CLAIX access and `--with-split` for optional `vpn-slice` installation. Interactive installs offer HPC setup; `--no-hpc` skips the question. For example: `sh install.sh --hpc --user ab123456`.

Runtime requirements are Bash and openconnect, plus the system's VPN route script and Linux's `ip` command for whole-machine tunnels. There is no Python, Go, or `secret-tool` requirement. HPC additionally needs OpenSSH and `ocproxy`. Split tunneling optionally uses `vpn-slice`, which has its own Python requirements. Linux encrypted storage needs a running Secret Service provider such as GNOME Keyring, reachable over a session D-Bus; unencrypted storage does not.

The installer never uses sudo. If a system package is missing, it tells you the command to install it and stops. It warns if the destination directory is missing from your PATH. From a checkout, run `make build` first; the installer uses `dist/rwth-vpn` when present.

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

`rwth-vpn secrets` lets you choose encrypted or unencrypted storage, and shows where to get each secret. `rwth-vpn secrets --no-hpc` only asks for VPN credentials and skips SSH setup. Use `--hpc` to enable HPC setup later. Existing credentials are retained when skipping HPC.

| Secret | Where to get it |
|---|---|
| RWTH username | Your TIM ID, e.g. `ab123456` |
| VPN password | [IdM Selfservice](https://idm.rwth-aachen.de/selfservice/). Not your SSO password. |
| VPN TOTP secret | [IdM Selfservice](https://idm.rwth-aachen.de/selfservice/) → Token Manager → new *Authenticator app (TOTP)* token |
| HPC password | [RegApp](https://regapp.itc.rwth-aachen.de/) |
| HPC TOTP secret | [RegApp](https://regapp.itc.rwth-aachen.de/), the HPC's own second factor |

When you create a TOTP token, copy the secret text (`JBSWY3DP…`) or the `otpauth://` link. Scan the same QR code with your phone too, so you can still log in by hand. `rwth-vpn totp` (or `rwth-vpn totp hpc`) prints the current code, which should match your phone.

Choose the storage mode explicitly:

```sh
rwth-vpn secrets --mode encrypted --no-hpc
rwth-vpn secrets --mode unencrypted --no-hpc
```

- **Encrypted:** macOS login Keychain or Linux Secret Service, under service `rwth-vpn`. Linux uses D-Bus directly through the bundled Go keyring library. There is no silent fallback to a file.
- **Unencrypted:** `~/.config/rwth-vpn/secrets` (or `$XDG_CONFIG_HOME/rwth-vpn/secrets`, mode 0600). Values are base64 encoded, not encrypted. This mode works unattended without an unlocked keyring.

Encryption at rest depends on your keyring being protected with a password. The app does not save that unlock password.

Changing modes copies and verifies credentials before saving the new mode and removing the original credentials. Existing installations keep their original backend until you choose a mode. Stored values include usernames, passwords, and TOTP secrets.

If encrypted storage is locked, approve the keyring prompt or run `rwth-vpn secrets --unlock` in a terminal, including over SSH. Before saving encrypted mode for the first time, use `rwth-vpn secrets --mode encrypted --unlock`. Terminal unlocking supports macOS Keychain and GNOME Keyring; other Secret Service providers need their own unlock workflow. Headless Linux needs a running session D-Bus and the same keyring service for setup and later SSH sessions. SSH public-key login does not automatically provide the keyring password. Unattended access fails with unlock guidance if the keyring is unavailable; keyring operations time out rather than hanging indefinitely.

**Be aware:** this machine now holds both factors. Anyone with your user account on it gets your RWTH VPN and HPC access.

## Uninstall

Run `rwth-vpn uninstall` to remove the installed binary and its SSH configuration. Credentials and settings are kept for reinstalling. Use `rwth-vpn uninstall --purge` to also delete stored credentials (including keyring entries) and settings. Stop an active whole-machine tunnel with `rwth-vpn down` first. Uninstall closes the managed SSH sessions and rootless proxy; dependencies and diagnostic cache files are kept.

## Supported Platforms

- Release targets: Linux, macOS, FreeBSD, OpenBSD and NetBSD, on amd64 and arm64. Cross-compilation is checked; native validation has run on Linux.
- Debian, Ubuntu, Fedora: VPN and HPC dependencies are packaged.
- Arch, Manjaro, openSUSE, Void, Alpine, Gentoo, Solus: optional `ocproxy` is built from source when needed.
- NixOS: add `openconnect` and, for HPC, `ocproxy` to your config.
- macOS: VPN dependencies via Homebrew.
- BSD: unencrypted storage and full tunnel; encrypted storage currently supports Linux and macOS.

## Development and Releases

Go 1.23+ is required only for development. The executable embeds the existing Bash orchestration and replaces the Python helpers with compiled Go networking, TOTP, and credential storage. It does not extract a script onto disk. TOTP uses [pquerna/otp](https://github.com/pquerna/otp), and encrypted storage uses [go-keyring](https://github.com/zalando/go-keyring). These libraries are compiled into the executable.

```sh
make build       # dist/rwth-vpn
make test        # unit and CLI integration tests; no real credentials or VPN needed
make check       # go vet and shell syntax checks
sh tests/secret-service.sh dist/rwth-vpn  # optional real, isolated Linux keyring test
make release VERSION=v0.1.0  # local binaries, pinned installer, and SHA-256 files
make publish VERSION=v0.1.0  # validate, build locally, push tag, upload release
```

Releases are built and uploaded locally; no GitHub Actions credits are needed. Update `scripts/release-notes.md`, commit the source, and run `make publish VERSION=v0.1.0` (choose a new version for subsequent releases). Publishing requires Go, Git, Make, and authenticated GitHub CLI (`gh`), and pushes the current branch plus an annotated release tag. The command refuses a dirty working tree or an existing tag and runs checks and tests before building. If uploading fails after the tag is pushed, retry with `gh release create VERSION dist/rwth-vpn_* dist/install.sh dist/install.sh.sha256 --verify-tag --notes-file scripts/release-notes.md`, replacing VERSION with that tag. There are no automatic Actions workflows. Tests cover RFC TOTP vectors, replay prevention, credential permissions and concurrency, keyring errors, storage migration failures, SOCKS framing, VPN-only setup, uninstall, and standalone download/checksum failures. The optional Linux keyring test needs `dbus-run-session` and `gnome-keyring-daemon`, and uses a private bus and temporary home directory. macOS Keychain access and real RWTH connections still need platform testing. The Docker lab tests full tunnels and SSH against local VPN and SSH servers.

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
