# rwth-vpn

RWTH Aachen VPN and HPC access from any Linux, macOS or BSD machine, built on
[openconnect](https://www.infradead.org/openconnect/). It runs alongside
Tailscale, needs no Cisco client, and asks for no TOTP code once set up.

```sh
ssh claix 'squeue --me'   # HPC: no sudo, no password, no TOTP; VPN starts on demand
rwth-vpn up               # whole machine via RWTH (paper access); asks for sudo
rwth-vpn status
rwth-vpn down
```

## Setup (once per machine, no sudo)

```sh
git clone git@github.com:ugnmura/rwth-vpn.git ~/src/rwth-vpn
~/src/rwth-vpn/install.sh
```

The installer puts everything in your home directory:
- links `rwth-vpn` into `~/.local/bin` (Homebrew's `bin` on macOS);
- builds `ocproxy` if your distro doesn't package it;
- sets up `~/.ssh` for the HPC;
- asks for your secrets.

It never runs sudo. If system packages are missing, it prints the one command
to install them (e.g. `sudo apt install openconnect ocproxy python3`) and
stops, so you can run that and rerun it. On macOS it uses Homebrew directly.

### Where to get the secrets

`rwth-vpn secrets` asks for these and shows where to get each one, with links. `rwth-vpn secrets --where` prints just that guide, and `rwth-vpn test-login` checks the stored VPN login. Enter only what
you use: VPN-only use doesn't need the HPC entries.

| Secret | Where | Notes |
|---|---|---|
| RWTH username | your TIM ID, e.g. `ab123456` | |
| VPN password | [IdM Selfservice](https://idm.rwth-aachen.de/selfservice/): set or change the VPN password there | separate from your RWTH SSO password |
| VPN TOTP secret | [IdM Selfservice](https://idm.rwth-aachen.de/selfservice/) → Token Manager → new token → *Authenticator app (TOTP)* | see below |
| HPC username | [RegApp](https://regapp.itc.rwth-aachen.de/) | usually the same `ab123456` |
| HPC password | [RegApp](https://regapp.itc.rwth-aachen.de/): your HPC account's password | separate from the VPN password |
| HPC TOTP secret | [RegApp](https://regapp.itc.rwth-aachen.de/): set up the HPC second factor (TOTP) | the HPC has its own tokens |

**Getting a TOTP secret.** When you create the token, the page shows a QR code
and the secret as text. Copy the text (base32, e.g. `JBSWY3DP…`) or the
`otpauth://…` link into `rwth-vpn secrets`. Also scan the same QR code with
your phone's authenticator app, so you can still log in by hand. Then enter
one code to finish creating the token. Keep the default SHA1 / 6 digits;
otherwise paste the `otpauth://` link, which carries those settings.
`rwth-vpn totp` (or `rwth-vpn totp hpc`) prints the current code: it should match your phone.

RWTH's guides: [VPN with authenticator app](https://help.itc.rwth-aachen.de/en/service/vbf6fx0gom76/article/34c748946f63455090ede2dd5a7a49e5),
[HPC login](https://help.itc.rwth-aachen.de/service/rhr4fjjutttf/article/fb61d6c86ae245b5b7bba8c0cb7db6eb/).

**Where they're stored.** On macOS they go in the login Keychain (service
`rwth-vpn`). Elsewhere they go in `~/.config/rwth-vpn/secrets` (mode 0600),
because keyring daemons need an unlocked desktop session, which agents and
headless machines don't have.

The trade-off: with the TOTP seed stored next to the password, this machine
holds both factors. Anyone with your user account on it gets your RWTH VPN
and HPC access. Only store secrets on machines you'd trust with that.

## HPC over ssh

`ssh-setup`, which the installer runs, adds `Include ~/.ssh/rwth-vpn.conf` to
your ssh config. That file defines:

| Name | Host |
|---|---|
| `claix` | login23-1.hpc.itc.rwth-aachen.de |
| `login23-*`, `copy23-*` | `<name>.hpc.itc.rwth-aachen.de` |

So `ssh claix`, `ssh login23-2`, `scp file copy23-1:` and `rsync -e ssh` all
work, as does VS Code Remote-SSH. On first use:
1. If the whole-machine tunnel isn't up, a **rootless VPN session** starts:
   openconnect + ocproxy, a SOCKS proxy only ssh uses. The rest of the machine
   is untouched and no sudo is needed.
2. One **master connection** logs in, answering the password and TOTP prompts
   from your secrets, and stays open for 8 hours.
3. Every later `ssh`/`scp`/`rsync` to the HPC reuses it instantly.

### For agents

Agents can use the HPC exactly like a human. There are no special flags, and
`-o BatchMode=yes` is fine:

```sh
ssh claix 'sbatch job.sh'
rsync -az results/ claix:work/results/
```

Parallel calls share one VPN session and one login. If something's off,
`rwth-vpn status` shows the sessions, and the logs are
`~/.cache/rwth-vpn/{proxy,ssh}.log` (or under `$XDG_RUNTIME_DIR`).

## Whole-machine tunnel (`rwth-vpn up`)

This is for paper access (IEEE, Springer, Elsevier, … check that your traffic
comes from RWTH) and anything else that isn't ssh. It's the only part that
needs root, and it asks for sudo, or doas.

- **Default (full tunnel):** all traffic leaves through RWTH. On Linux:
  - **Tailscale keeps working directly:** its own traffic keeps using your
    normal uplink.
  - **IPv6 is blocked while connected** if the VPN gives none, so dual-stack
    sites can't see your home address.
  - Docker follows the host.
- **`--split`:** only RWTH networks go through the VPN (the server's
  split-include list plus 134.61/16, 134.130/16, 137.226/16). Set
  `RWTH_MODE=split` in `~/.config/rwth-vpn/config` to make it the default.

While `up` is active, ssh to the HPC goes through it directly. `up` replaces
the rootless ssh session, since RWTH allows only one VPN session.

## Platforms

| Platform | Notes |
|---|---|
| Arch / Manjaro | ocproxy built from source |
| Debian / Ubuntu, Fedora | everything packaged |
| openSUSE, Void, Alpine, Gentoo, Solus | ocproxy built from source |
| NixOS | add `openconnect` (+ `ocproxy`) to your config |
| macOS | Homebrew; secrets in the Keychain |
| FreeBSD / OpenBSD / NetBSD | full tunnel only (vpn-slice is Linux/macOS) |

## Why stock openconnect fails

RWTH's VPN is a Cisco ASA that answers **HTTP 404** to any client whose
User-Agent isn't `AnyConnect <OS> <version>`. Stock openconnect sends
`Open AnyConnect VPN Agent`, so you get:

```
Unexpected 404 result from server
Failed to complete authentication
```

`rwth-vpn` sends `--useragent="AnyConnect Linux_64 5.1.6.103"`.
`rwth-vpn check` verifies the workaround without logging in. Run it first if
logins suddenly break.

## Known limitations

- On macOS and the BSDs, `up` uses plain vpnc-script. IPv6 isn't blocked
  there, so a dual-stack publisher may still see your home IPv6 address.
- In full mode, vpnc-script points `/etc/resolv.conf` at RWTH's DNS. If
  DHCP rewrites it on lease renewal, RWTH-internal names stop resolving until
  you reconnect.
- The askpass helper recognises prompts mentioning password,
  OTP/token/one-time/second factor/verification code. It refuses anything
  else rather than guess. If RWTH words a prompt differently,
  `~/.cache/rwth-vpn/ssh.log` shows it.
