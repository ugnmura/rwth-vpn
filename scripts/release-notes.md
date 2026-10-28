Refactored source layout and a command line that explains itself.

- Go code now lives under cmd/rwth-vpn and focused internal packages; integration tests live under tests/integration.
- Command options use Go's built-in flag parser, with generated usage and option listings. No new dependencies.
- Use rwth-vpn COMMAND --help or rwth-vpn help TOPIC for credential storage, unlocking, SSH setup, configuration, and troubleshooting.
- Invalid arguments and help are handled before loading configuration or accessing credentials.
- The installer now supports --help. The README is a short install/build/release guide.

Download the attached install.sh and run `sh install.sh`. Add `--hpc` for CLAIX access. The installer selects v0.1.1 and verifies the matching binary's SHA-256 checksum.

Runtime requirements remain Bash, openconnect, and system routing tools. No Python, Go compiler, or secret-tool is required by the executable.

Linux validation includes race tests, a real isolated Secret Service, and the full Docker VPN/SSH suite. All ten OS/architecture targets are cross-compiled; native macOS/BSD validation remains outstanding.
