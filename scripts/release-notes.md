Interactive setup, safer credential updates, and VPN/SSH recovery fixes.

- Run rwth-vpn for the menu. Select with a number, arrow keys, or j/k.
- Credential edits stay in memory until the final save confirmation. After saving, setup tests VPN login and optional HPC login.
- Agents can preview and apply a private setup profile with rwth-vpn configure. See docs/AGENTS.md for secure human input and confirmation.
- TLS-only VPN transport is now the default; --dtls opts into UDP transport. Disconnect restores saved routing and DNS after an unexpected VPN exit.
- SSH detects masters from an earlier VPN session and reconnects. Gateway dialing uses IPv4/IPv6 fallback, and authentication waits can be canceled.
- Help uses conventional double-dash options. The README includes installation and connect/disconnect examples.

Run the attached install.sh with Bash/openconnect and system VPN routing tools installed. The installer selects v0.1.2 and verifies the binary's SHA-256 checksum.

Linux validation includes Go race tests, isolated Secret Service tests, and Docker VPN/SSH regression checks. Release binaries are cross-compiled for Linux, macOS, FreeBSD, OpenBSD, and NetBSD on amd64 and arm64. Native macOS/BSD validation remains outstanding.
