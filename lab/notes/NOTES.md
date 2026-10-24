# RWTH VPN on the Pi: notes

## 2026-10-20: root cause found
- The ASA picks its response based on the **User-Agent**:
  - `Open AnyConnect VPN Agent v9.12` (openconnect default) → `POST /` 404, then falls back to `/+webvpn+/index.html` 404 → "Failed to complete authentication".
  - `Cisco AnyConnect VPN Agent for Windows …` and `Mozilla/5.0` → 404.
  - `AnyConnect Linux_64 5.1.6.103` → 200 with a normal aggregate-auth XML form.
- `/+CSCOE+/logon.html` returns 200, but the body is `Internal Server Error` (the clientless portal is effectively off). It doesn't show the ASA working.
- With `--useragent="AnyConnect Linux_64 5.1.6.103" --version-string=5.1.6.103 --os=linux-64`, openconnect 9.12 gets the form:
  - fields: username, password, `group_list`
  - groups: `RWTH-VPN (Full Tunnel)` (default, tunnel-group `TunnelGroup_RWTH_Full`), `RWTH-VPN (Split Tunnel)`
  - the server also advertises SSO auth methods, but the username/password form is offered.
  - The TOTP prompt probably comes as a second form after the password. Not yet seen; needs real credentials.
- So Cisco Secure Client is not needed, and there's no need for a VPN on another machine with Tailscale subnet routing.
- Raw logs: `notes/diag/oc1.log` (default UA, fails), `notes/diag/oc2.log` (AnyConnect UA, form received).

## Setup
- vpn-slice 0.16.1 installed with `~/.local/bin/uv tool install vpn-slice`.
- `~/rwth-vpn/rwth-vpn.sh`: openconnect with the UA fix, Split Tunnel group, interface `tun-rwth`, vpn-slice routing only 134.130.0.0/16 and 137.226.0.0/16.
  - Default route, /etc/resolv.conf, Tailscale rules and table 52, and Docker bridges are left alone.
  - No credentials are stored. openconnect prompts for password and TOTP.
- IPv6 RWTH range (2a00:8a60::/32) is deliberately **not** sliced: the VPN gateway itself is reached over IPv6 at 2a00:8a60:0:6000:…, so routing it into the tunnel would cut the tunnel's own transport.

## HPC (CLAIX)
- The login nodes are in **134.61.0.0/16**, which TASK.md's range list was missing. It's now added to the script.
  - login23-1 134.61.44.117, login23-2 .44.115, login23-3 .45.29, login23-4 .45.27, login23-g-1 .46.249
  - login23-x-1 134.61.193.137, copy23-1 134.61.193.141
- Without the VPN: TCP/22 to login23-1 → connection refused (expected from outside RWTH).
- login23-x-1 and copy23-1 also have AAAA records (2a00:8a60:1:a000::/64). The VPN only routes IPv4, so use `AddressFamily inet` for `*.hpc.itc.rwth-aachen.de` in ~/.ssh/config. Otherwise ssh could go over native IPv6, outside the tunnel.

## Scope (user decision, 2026-10-20)
- The user wants to reach **everything in the RWTH network**, not just a few hosts.
- The script now runs vpn-slice with `-S` (every split-include subnet the ASA pushes for the Split Tunnel group) and `-I` (the VPN's internal subnet), plus all RWTH IPv4 /16s as a fallback.
- Internet traffic still goes out over Wi-Fi, which keeps Tailscale and Docker safe.
- Full tunnel (all traffic via RWTH) isn't set up. Only needed if a site checks for an RWTH source IP and isn't in the pushed routes.

## 2026-10-20: turned into a reusable project
- The user wants a generic tool for all their machines (any Linux, macOS, BSD) that runs alongside Tailscale and "just works", including paper access.
- Repo: **github.com/ugnmura/rwth-vpn** (private), checked out at `~/src/rwth-vpn`. It replaces the earlier `~/rwth-vpn/rwth-vpn.sh` (deleted).
- `rwth-vpn up` uses the Full Tunnel group, because publishers need an RWTH source IP. On Linux, the hook (`lib/hook.sh`, now `rwth-vpn _hook`):
  - keeps Tailscale's underlay (fwmark 0x80000) on the uplink: rules 5200/5201, table 7337;
  - blocks native IPv6 when the VPN gives none: rules 5280/5281, table 7338, after Tailscale's 5270 tailnet rule. An unreachable route in main ends IPv6 rule lookup despite suppress_prefixlength, so the block needs its own table.
- `rwth-vpn up --split` uses vpn-slice with -S -I plus the RWTH /16s.
- Login runs as the user. The cookie is piped to the root openconnect (`--cookie-on-stdin`). The gateway is pinned to IPv4 with `--resolve`.
- Tests:
  - Hook routing tested in an Alpine netns with the real vpnc-script and Tailscale-like rules: all cases pass and disconnect leaves nothing behind.
  - `install.sh` (POSIX sh) passes in containers: Debian, Ubuntu, Fedora, openSUSE, Alpine, Void-glibc (on the Pi, aarch64). Arch, Void-musl and Gentoo were run on msi (x86_64).
- Still unverified: a real login (TOTP form), macOS, the BSDs.

## To do / verify after first connect
- [ ] Install on the Pi (needs sudo, user runs it): `~/src/rwth-vpn/install.sh --user ab123456`
- [ ] `rwth-vpn up`, then enter the password and TOTP. Note what the second form looks like.
- [ ] `rwth-vpn status` shows whether the VPN gave IPv6. Then `ip route`, `ip rule`, `ip -6 rule`.
- [ ] `tailscale status`: macbook still direct 192.168.11.9, msi still direct.
- [ ] `curl -4 https://ifconfig.co` shows an RWTH IP. `curl -6` fails fast (when blocked).
- [ ] `ssh login23-1.hpc.itc.rwth-aachen.de` works. Docker containers still reach the internet.
- [ ] `rwth-vpn down` restores routes, rules and resolv.conf.

## 2026-10-20 (later): prompt-free ssh for humans and agents
- `ssh claix` works without sudo or prompts: on demand, a rootless VPN session (openconnect + ocproxy SOCKS), then an HPC ControlMaster logged in via askpass, kept for 8 h. Secrets come from `rwth-vpn secrets` (macOS Keychain / 0600 file elsewhere).
- The HPC has its OWN password and TOTP (RegApp). The VPN's come from IdM Selfservice + Token Manager. `rwth-vpn secrets` shows the links.
- install.sh never uses sudo; it prints the package command instead. Only `up` needs root.
- Lab: /home/pi/src/rwth-vpn-lab (ocserv + sshd with TOTP, Docker). Full suite passed. Rerun: setup.sh, tests.sh, teardown.sh.
- 15-min backoff after a failed automatic login (lockout protection for agents).
- 2026-10-20: vpn.rwth-aachen.de stopped answering our home IPv4 (134.130.5.231), from the Pi and msi alike; IPv6 still fine. Possibly ASA shunning after ~10 abandoned `check` logins. gateway_addr now falls back to IPv6.
- Unverified until the first real login: the ASA's prompt order (password, then TOTP?), the CLAIX prompt texts, and RWTH's simultaneous-session limit.
