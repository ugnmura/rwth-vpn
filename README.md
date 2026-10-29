# rwth-vpn

Connect to the RWTH VPN and CLAIX from your terminal.

## Install

Needs Bash and openconnect.

```sh
sh -c "$(curl -fsSL https://github.com/ugnmura/rwth-vpn/releases/latest/download/install.sh)"
```

Run `rwth-vpn` for the interactive menu, or `rwth-vpn --help` for commands and flags.

## Usage

Connect:

```sh
rwth-vpn up
```

Disconnect:

```sh
rwth-vpn down
```

## Agents

See [docs/AGENTS.md](docs/AGENTS.md) for setup with human credential input.

## Build

Requires Go 1.23+ and Make.

```sh
make build
./dist/rwth-vpn --help
make test
```
