# rwth-vpn

RWTH Aachen VPN and optional CLAIX access. Uses openconnect.

Personal tool. Tested on Linux. RWTH can change things at any time.

## Install

Requires Bash, openconnect and system VPN routing tools. The installer lists missing packages.

```sh
sh -c "$(curl -fsSL https://github.com/ugnmura/rwth-vpn/releases/latest/download/install.sh)"
```

Run `rwth-vpn` for the interactive menu, or `rwth-vpn --help` for commands and flags.

## Agents

See [docs/AGENTS.md](docs/AGENTS.md) for setup with human credential input.

## Build

Requires Go 1.23+ and Make.

```sh
make build
./dist/rwth-vpn --help
make test
```
