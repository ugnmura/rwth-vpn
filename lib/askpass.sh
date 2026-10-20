#!/bin/sh
# SSH_ASKPASS helper: answers the HPC login prompts from stored secrets.
# ssh passes the prompt text as $1; we print the answer, or fail on prompts we
# don't recognise so ssh stops instead of sending something wrong.

LIB_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib/secrets.sh
. "$LIB_DIR/secrets.sh"

# OpenSSH prefixes "(user@host) "; drop it so a user or host name containing
# "otp"/"token" can't change which answer we give.
prompt=$(printf '%s' "$1" | sed 's/^([^)]*) *//' | tr '[:upper:]' '[:lower:]')

case "$prompt" in
    # CLAIX asks "Two-factor code:" (before the password).
    *two-factor*|*two\ factor*|*otp*|*token*|*one-time*|*one\ time*|*second\ factor*|*2fa*|*verification\ code*|*authenticator*)
        # The HPC has its own TOTP tokens (RegApp), separate from the VPN's.
        secret_get hpc_totp_secret | python3 -I "$LIB_DIR/totp.py" --fresh
        ;;
    *passphrase*)
        # Key passphrases belong to ssh-agent, not to us.
        exit 1
        ;;
    *password*)
        # No fallback to the VPN password: a wrong guess counts as a failed login.
        secret_get hpc_password
        echo
        ;;
    *)
        echo "rwth-vpn askpass: unrecognised prompt: $1" >&2
        exit 1
        ;;
esac
