# rwth-vpn

Connect to the **RWTH Aachen VPN** from any Linux, macOS or BSD machine with
[openconnect](https://www.infradead.org/openconnect/). No Cisco Secure Client
needed, and it runs alongside Tailscale.

```sh
rwth-vpn up        # VPN password + TOTP code, then you're in
rwth-vpn status
rwth-vpn down
```

## Install

```sh
git clone git@github.com:ugnmura/rwth-vpn.git ~/src/rwth-vpn
~/src/rwth-vpn/install.sh            # optional: --user ab123456 --no-split
```

The installer:
- installs openconnect, vpnc-script, bash and (on Linux) iproute2 with the system package manager;
- installs vpn-slice with `uv tool` or `pipx`, used for `--split`;
- links `rwth-vpn` into `/usr/local/bin`;
- optionally saves your username in `~/.config/rwth-vpn/config`.

| Platform | Package manager | Status |
|---|---|---|
| Arch / Manjaro | pacman | tested (aarch64) |
| Debian / Ubuntu | apt | installer tested in container |
| Fedora | dnf | installer tested in container |
| RHEL / Alma / Rocky | dnf/yum | needs EPEL for openconnect |
| openSUSE | zypper | installer tested in container |
| Void | xbps | installer tested in container |
| Alpine | apk | installer tested in container |
| Gentoo, Solus | emerge, eopkg | best effort |
| NixOS | – | add `openconnect` to your config; the installer does the rest |
| macOS | Homebrew or MacPorts | full + split |
| FreeBSD / OpenBSD / NetBSD | pkg, pkg_add, pkgin | full mode only (vpn-slice is Linux/macOS) |

Root comes from `sudo`, or `doas` where there's no sudo.

## Modes

**`rwth-vpn up`** (full tunnel, the default). All traffic leaves through RWTH,
so publishers (IEEE, Springer, Elsevier, …) see a university address, and every
RWTH-internal host and the HPC cluster are reachable. On Linux:
- **Tailscale keeps working directly.** Its own traffic (fwmark `0x80000`) keeps
  using your normal uplink, so peers on the LAN or reachable by NAT traversal
  stay direct instead of detouring through Aachen.
- **IPv6 is blocked while connected if the VPN gives none.** Otherwise
  dual-stack sites would see your home IPv6 address and paper access would
  silently fail. Tailscale's own IPv6 and on-link IPv6 stay up.
- Docker containers follow the host, so they're on the VPN too.

**`rwth-vpn up --split`**: only RWTH networks go through the VPN, meaning the
routes the server pushes for its Split Tunnel group plus 134.61.0.0/16,
134.130.0.0/16 and 137.226.0.0/16. Everything else stays local and fast. Use this
when you only need the HPC or internal services. To make it the default, set
`RWTH_MODE=split` in the config.

## How it works / why stock openconnect fails

RWTH's VPN is a Cisco ASA that answers **HTTP 404** to any client whose
User-Agent isn't `AnyConnect <OS> <version>`. Stock openconnect identifies as
`Open AnyConnect VPN Agent`, which produces:

```
Unexpected 404 result from server
Failed to complete authentication
```

`rwth-vpn` passes `--useragent="AnyConnect Linux_64 5.1.6.103"`, and the ASA
then serves the normal login form (username, password, then TOTP).
`rwth-vpn check` verifies this without logging in. Run it first if things
break, since RWTH may change the filter again.

Login (`openconnect --authenticate`) runs as your user. Only the tunnel runs as
root, and it gets the session cookie through a pipe. Your password and TOTP are
never stored, and the cookie never shows up in `ps`. The tunnel log is at
`/var/log/rwth-vpn.log`.

## HPC (CLAIX)

```
# ~/.ssh/config
Host login23-* copy23-*
    HostName %h.hpc.itc.rwth-aachen.de
    User ab123456
```

Then `rwth-vpn up && ssh login23-1`. In `--split` mode, also add
`AddressFamily inet`: some HPC hosts have IPv6 addresses, and split mode only
routes IPv4.

## Known limitations

- On macOS and the BSDs, full mode uses plain vpnc-script. IPv6 isn't blocked
  there, so dual-stack publisher sites may still see your home IPv6 address.
  If paper access fails, turn IPv6 off for the network while connected.
- In full mode, vpnc-script points `/etc/resolv.conf` at RWTH's DNS. If DHCP
  (dhcpcd, NetworkManager) rewrites it during a lease renewal, public DNS keeps
  working but RWTH-internal names stop resolving. Reconnect to restore them.
- The VPN session follows RWTH's server-side timeouts. After a long idle
  period, run `rwth-vpn up` again.
