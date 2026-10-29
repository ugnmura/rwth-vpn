# Agent setup

Agents can configure rwth-vpn without running the interactive `secrets` wizard.
Run `rwth-vpn configure --help` to check that the installed version supports this interface.

## Ask the human

Ask whether they want VPN only or VPN and CLAIX, and encrypted or unencrypted storage.
Encrypted storage uses the OS keyring; unlocking it may require human interaction.
Unencrypted storage uses a private local file.

Request credentials through the agent host's secure credential form, if available.
That form must deliver values directly to a private local file or protected stdin.
An ordinary chat question or multiple-choice input is not a password field. If the
host has no secure credential input, let the human enter credentials locally using
`rwth-vpn secrets`, or create the private JSON file themselves. Do not ask them to
paste passwords or TOTP seeds into chat.

Collect the VPN username, VPN password, and VPN TOTP seed (or otpauth URI).
For CLAIX, also collect the HPC username, HPC password, and separate HPC TOTP seed.
The HPC username can be omitted to use the VPN username. A six-digit authentication
code is not a TOTP seed. Explain which service each requested credential belongs to.

## Preview, then save

The secure input provider should create a regular file readable only by its owner
(0600 or 0400). Its JSON has this shape; placeholders below are not real credentials:

```json
{
  "mode": "encrypted",
  "hpc": false,
  "credentials": {
    "user": "<VPN username>",
    "vpn_password": "<VPN password>",
    "totp_secret": "<VPN TOTP seed>"
  }
}
```

Optional credential keys are `hpc_user`, `hpc_password`, and `hpc_totp_secret`.
Omitted credential keys preserve existing values; `null` clears a value.
Use `"mode": "unencrypted"` for file storage. Never put secret values in command
arguments, environment variables, tool-call text, shell literals, logs, or commits.
Use only the private file's path in agent commands. Do not read its contents into
the conversation.

```sh
rwth-vpn configure --file "$SECURE_PROFILE" --json
```

This previews without saving. Output contains field names and actions, never
credential values. Check `ready` and `missing`, and present the storage choice,
HPC choice, and changes to the human. Request final confirmation before applying.
Cancellation must leave existing credentials and preferences unchanged.

```sh
rwth-vpn configure --file "$SECURE_PROFILE" --apply --json
```

`--stdin` is also available for a secure provider that can supply a protected
stream directly. The CLI cannot create a secure host UI or enforce approval on
the agent's behalf: the agent must obtain approval before using `--apply`.
Remove the temporary credential file after completion or cancellation using the
provider's cleanup mechanism. Never retain it as an artifact.

## Verify the requested access

Configuration checks field completeness and TOTP format; it does not authenticate
every credential. For CLAIX, run `rwth-vpn ssh-setup`, then test the requested SSH
access once. For a requested full VPN connection, run `rwth-vpn up` and inspect
`rwth-vpn status`. Follow CLI diagnostics when authentication fails; do not loop
retries, remove login backoff, or bypass TOTP reuse protection.

Do not overwrite existing credentials during troubleshooting without a fresh
preview and confirmation. Avoid disrupting a working host VPN for tests; use
the isolated Docker lab when developing network changes.

## Development

Keep the README short; put detailed procedures here or in CLI help. Prefer Go's
standard library and small dependencies. Keep executable entry points in `cmd/`
and implementation in `internal/`. Run `make test` and `make check`; use the lab
for routing and SSH integration changes. Release tooling is in `scripts/` and
the Makefile; publishing is a separate explicit action.
