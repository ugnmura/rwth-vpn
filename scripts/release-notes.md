A single downloadable executable for RWTH VPN access, with optional CLAIX HPC setup.

- No Python, Go compiler, or secret-tool required at runtime. Bash, openconnect, and system routing tools remain required.
- Encrypted credentials use Linux Secret Service or macOS Keychain; unencrypted storage uses a mode-0600 file.
- VPN-only installation is the default. HPC and split tunneling are opt-in.
- Includes credential migration, interactive unlock guidance, and uninstall with optional credential purge.
- The attached install.sh selects the matching release binary and verifies its SHA-256 checksum.

Download install.sh and run `sh install.sh`. Add `--hpc` for CLAIX or `--with-split` for optional vpn-slice installation.

Validated on Linux with race tests, an isolated real Secret Service, and all 11 Docker VPN/SSH integration cases. macOS and BSD binaries are cross-compiled; native runtime validation remains outstanding.
