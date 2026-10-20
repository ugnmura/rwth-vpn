# Secret storage for rwth-vpn, sourced by rwth-vpn and askpass.sh (POSIX sh).
#
# macOS: login Keychain (service "rwth-vpn"). Elsewhere: a 0600 file, because
# keyring daemons (secret-service, gpg-agent) need an unlocked GUI session,
# which agents on headless machines don't have.
#
# Names: user, vpn_password, totp_secret, hpc_user, hpc_password

SECRETS_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/secrets"

secret_backend() {
    if [ "$(uname -s)" = Darwin ] && command -v security >/dev/null 2>&1; then
        echo keychain
    else
        echo file
    fi
}

secret_get() {
    case "$(secret_backend)" in
        keychain) security find-generic-password -s rwth-vpn -a "$1" -w 2>/dev/null;;
        file)
            [ -r "$SECRETS_FILE" ] || return 1
            # Values are base64 so any character survives the line format.
            v=$(sed -n "s/^$1=//p" "$SECRETS_FILE" | head -n 1)
            [ -n "$v" ] || return 1
            printf '%s' "$v" | base64 -d 2>/dev/null || printf '%s' "$v" | base64 -D
            ;;
    esac
}

# secret_set NAME VALUE (empty VALUE deletes)
secret_set() {
    case "$(secret_backend)" in
        keychain)
            security delete-generic-password -s rwth-vpn -a "$1" >/dev/null 2>&1
            # `security` has no reliable non-tty way to read the value, so it
            # is briefly in argv; this only runs during `rwth-vpn secrets`.
            [ -z "$2" ] || security add-generic-password -s rwth-vpn -a "$1" -U -w "$2"
            ;;
        file)
            (
                umask 077
                mkdir -p "$(dirname "$SECRETS_FILE")"
                tmp="$SECRETS_FILE.tmp.$$"
                { [ -r "$SECRETS_FILE" ] && grep -v "^$1=" "$SECRETS_FILE"; } >"$tmp" || true
                [ -z "$2" ] || printf '%s=%s\n' "$1" "$(printf '%s' "$2" | base64 | tr -d '\n')" >>"$tmp"
                mv "$tmp" "$SECRETS_FILE"
            )
            ;;
    esac
}

have_secret() { [ -n "$(secret_get "$1")" ]; }
