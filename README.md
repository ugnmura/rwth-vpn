# rwth-vpn

RWTH Aachen VPN and optional CLAIX access. Uses openconnect. Plays nicely with Tailscale.

Personal tool. Tested on Linux. RWTH can change things at any time.

## Install

Requires Bash, openconnect and system VPN routing tools. The installer lists missing packages.

```sh
curl -fsSL https://github.com/ugnmura/rwth-vpn/releases/latest/download/install.sh -o install.sh
sh install.sh
rwth-vpn --help
```

## Build

Requires Go 1.23+ and Make.

```sh
make build
./dist/rwth-vpn --help
make test
```

## Release

Built locally. No GitHub Actions.

```sh
make publish VERSION=v0.1.1
```

Update `scripts/release-notes.md` and commit first. Requires authenticated `gh`.
