#!/usr/bin/env bash
# Embedded VPN orchestration. Public options and help live in internal/cli.
# Keep compatible with Bash 3.2 (macOS).

set -euo pipefail

SERVER=vpn.rwth-aachen.de
# The ASA answers 404 to any client whose User-Agent isn't "AnyConnect <OS> <ver>",
# which is why stock openconnect fails with "Failed to complete authentication".
AC_VERSION=5.1.6.103
GROUP_FULL="RWTH-VPN (Full Tunnel)"
GROUP_SPLIT="RWTH-VPN (Split Tunnel)"
# RWTH IPv4 ranges, routed in --split mode in addition to what the server pushes.
SPLIT_ROUTES="134.61.0.0/16 134.130.0.0/16 137.226.0.0/16"

RUN_DIR=/var/run/rwth-vpn
PID_FILE=$RUN_DIR/openconnect.pid
STATE_FILE=$RUN_DIR/state
RECOVERY_FILE=$RUN_DIR/disconnect.env
LOG_FILE=/var/log/rwth-vpn.log

resolve_self() {
    local p=$1 l
    while [ -L "$p" ]; do
        l=$(readlink "$p")
        case "$l" in /*) p=$l;; *) p=$(dirname "$p")/$l;; esac
    done
    echo "$(cd "$(dirname "$p")" && pwd)/$(basename "$p")"
}
SELF=$(resolve_self "$0")


die() { echo "rwth-vpn: $*" >&2; exit 1; }
is_root() { [ "$(id -u)" -eq 0 ]; }

elevate_cmd() {
    if command -v sudo >/dev/null 2>&1; then echo sudo
    elif command -v doas >/dev/null 2>&1; then echo doas
    else die "need root: install sudo or doas, or run as root"
    fi
}

# Secret storage.
#
# Encrypted: macOS login Keychain or Linux Secret Service over session D-Bus.
# Unencrypted mode uses a 0600 base64 file.
#
# Names: user, vpn_password, totp_secret, hpc_user, hpc_password, hpc_totp_secret

SECRETS_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/secrets"
STORAGE_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/storage-mode"
PROFILE_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/setup-profile"
SECRET_NAMES="user vpn_password totp_secret hpc_user hpc_password hpc_totp_secret"

secret_backend() {
    local mode=${SECRET_MODE_OVERRIDE:-}
    if [ -z "$mode" ] && [ -f "$STORAGE_FILE" ]; then
        IFS= read -r mode <"$STORAGE_FILE" || true
        [ -n "$mode" ] || die "empty credential storage mode in $STORAGE_FILE; refusing to fall back"
    fi
    case "$mode" in
        unencrypted) echo file; return;;
        encrypted) ;;
        "")
            # Preserve pre-existing installations until the user chooses a mode.
            if [ -f "$SECRETS_FILE" ]; then echo file; return; fi;;
        *) die "invalid credential storage mode in $STORAGE_FILE";;
    esac
    if [ "$(uname -s)" = Darwin ]; then
        echo keychain
    elif [ "$mode" = encrypted ]; then
        case "$(uname -s)" in Linux) echo secret-service;; *) die "encrypted storage supports Linux and macOS";; esac
    else
        echo file
    fi
}

# Bound GUI prompts and unavailable D-Bus services, including unattended SSH.
secret_get() {
    "$SELF" __native secret-get "$(secret_backend)" "$SECRETS_FILE" "$1"
}

# Values travel through stdin, never command-line arguments.
secret_set() {
    printf '%s' "$2" | "$SELF" __native secret-set "$(secret_backend)" "$SECRETS_FILE" "$1"
}

have_secret() { [ -n "$(secret_get "$1")" ]; }

# Rootless session for ssh: openconnect + ocproxy (userspace TCP/IP) exposing
# the VPN as a SOCKS5 proxy. Per user, no root, no system routes touched.
USER_DIR="${XDG_RUNTIME_DIR:-$HOME/.cache}/rwth-vpn"
PROXY_PID=$USER_DIR/proxy.pid
PROXY_LOG=$USER_DIR/proxy.log
SSH_CONF="$HOME/.ssh/rwth-vpn.conf"
# After a failed automatic login, don't retry automatically for a while:
# agents retrying in a loop would otherwise lock the token or account.
VPN_FAIL_MARK=$USER_DIR/vpn-login-failed
HPC_FAIL_MARK=$USER_DIR/hpc-login-failed
BACKOFF_SECS=900

load_config() {
    local cfg="${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/config"
    # shellcheck disable=SC1090
    [ -r "$cfg" ] && . "$cfg"
    RWTH_USER=${RWTH_USER:-}
    if [ "${1:-}" != --no-secrets ] && [ -z "$RWTH_USER" ]; then
        RWTH_USER=$(secret_get user || true)
    fi
    RWTH_MODE=${RWTH_MODE:-full}
    RWTH_DTLS=${RWTH_DTLS:-0}
    case "$RWTH_DTLS" in 0|1) ;; *) die "RWTH_DTLS must be 0 or 1";; esac
    RWTH_SOCKS_PORT=${RWTH_SOCKS_PORT:-11080}
    # Overridable for testing against another AnyConnect server (e.g. ocserv).
    SERVER=${RWTH_SERVER:-$SERVER}
}

find_ocproxy() {
    command -v ocproxy 2>/dev/null && return
    [ -x "$HOME/.local/bin/ocproxy" ] && { echo "$HOME/.local/bin/ocproxy"; return; }
    return 1
}

# Log in and print openconnect's --authenticate output (COOKIE=..., etc.).
# With stored secrets this needs no input: password and a fresh TOTP code are
# piped in the order the ASA asks for them. Otherwise the user is prompted.
authenticate() {
    local group=$1 gw=$2 user=${3:-$RWTH_USER}
    # Read fresh: `rwth-vpn secrets` may have stored it after load_config ran.
    [ -n "$user" ] || user=$(secret_get user || true)
    local args=("${UA_ARGS[@]}" --resolve="$SERVER:$gw" --authgroup="$group")
    [ -n "$user" ] && args+=(--user="$user")
    if [ -n "$user" ] && have_secret vpn_password && have_secret totp_secret; then
        "$SELF" __native authenticate "$(secret_backend)" "$SECRETS_FILE" \
            --authenticate "${args[@]}" "$SERVER"
    elif [ ! -t 0 ]; then
        # No terminal to prompt on (agents, ssh ProxyCommand): fail clearly.
        echo "rwth-vpn: VPN secrets incomplete (need username, VPN password, TOTP secret): run 'rwth-vpn secrets'" >&2
        return 1
    else
        echo "Logging in to $SERVER ($group). Enter your VPN password, then the TOTP code." >&2
        echo "(Store them once with 'rwth-vpn secrets' to skip this.)" >&2
        openconnect --authenticate "${args[@]}" "$SERVER"
    fi
}

auth_field() {
    printf '%s\n' "$1" | sed -n "s/^$2='\(.*\)'$/\1/p"
}

UA_ARGS=(--useragent="AnyConnect Linux_64 $AC_VERSION" --version-string="$AC_VERSION" --os=linux-64)

# openconnect < 9.20 computes Cisco STRAP channel bindings wrongly over TLS 1.3
# (fixed upstream in 94e0b16c, "Use RFC9266 tls-exporter ... with TLSv1.3"), so
# RWTH's ASA accepts the login but answers the tunnel CONNECT with 401 "Cookie
# was rejected". Cap those versions at TLS 1.2 for openconnect only, through
# per-process config files; nothing system-wide changes.
tls_workaround() {
    command -v openconnect >/dev/null 2>&1 || return 0
    local v major minor
    v=$(openconnect --version 2>&1 | sed -n 's/^OpenConnect version v\{0,1\}\([0-9]*\)\.\([0-9]*\).*/\1 \2/p' | head -n 1)
    read -r major minor <<<"${v:-0 0}"
    [ "$((major * 100 + minor))" -lt 920 ] || return 0
    local dir=$USER_DIR
    if is_root; then dir=$RUN_DIR; mkdir -p "$dir"
    else mkdir -p "$dir" && chmod 700 "$dir"; fi
    if openconnect --version 2>&1 | grep -q 'Using OpenSSL'; then
        export OPENSSL_CONF=$dir/openssl-tls12.cnf
        printf '%s\n' 'openssl_conf = default_conf' '[default_conf]' 'ssl_conf = ssl_sect' \
            '[ssl_sect]' 'system_default = system_default_sect' \
            '[system_default_sect]' 'MaxProtocol = TLSv1.2' >"$OPENSSL_CONF"
    else
        export GNUTLS_SYSTEM_PRIORITY_FILE=$dir/gnutls-tls12.conf
        printf '%s\n' '[overrides]' 'disabled-version = tls1.3' >"$GNUTLS_SYSTEM_PRIORITY_FILE"
    fi
}

is_running() {
    # ps, not kill -0: the tunnel runs as root, so kill -0 from the user fails (EPERM).
    local pid
    pid=$(cat "$PID_FILE" 2>/dev/null) || return 1
    process_running "$pid"
}

process_running() {
    local state
    state=$(ps -p "$1" -o stat= 2>/dev/null) || return 1
    # Containers without a reaping init can retain an exited process as a zombie.
    case "$state" in ""|*Z*|*X*) return 1;; *) return 0;; esac
}

find_vpnc_script() {
    local p
    # openconnect knows where its own distro put vpnc-script (incl. Nix store paths).
    p=$(openconnect --help 2>&1 | sed -n 's/^ *default: "\(.*vpnc-script[^"]*\)".*/\1/p' | head -n 1)
    [ -n "$p" ] && [ -x "$p" ] && { echo "$p"; return; }
    for p in /etc/vpnc/vpnc-script /usr/share/vpnc-scripts/vpnc-script \
             /usr/libexec/vpnc-scripts/vpnc-script /usr/local/etc/vpnc/vpnc-script \
             /usr/local/sbin/vpnc-script /opt/homebrew/etc/vpnc/vpnc-script \
             /opt/local/etc/vpnc/vpnc-script /usr/pkg/etc/vpnc/vpnc-script; do
        [ -x "$p" ] && { echo "$p"; return; }
    done
    return 1
}

find_vpn_slice() {
    local h p
    command -v vpn-slice 2>/dev/null && return
    for h in "${SUDO_USER_HOME:-}" "$HOME"; do
        [ -n "$h" ] || continue
        for p in "$h/.local/bin/vpn-slice" "$h/.local/share/uv/tools/vpn-slice/bin/vpn-slice" \
                 "$h/.local/pipx/venvs/vpn-slice/bin/vpn-slice"; do
            [ -x "$p" ] && { echo "$p"; return; }
        done
    done
    return 1
}

# Pick the gateway address that actually answers on 443, IPv4 first. Pinning
# it (--resolve) keeps the tunnel's own transport on that address; full mode's
# IPv6 block exempts it via the host route vpnc-script adds. Falling back to
# IPv6 matters: RWTH's ASA has been seen to stop answering a home IPv4 while
# IPv6 still worked. Bounded, so an unreachable server fails in seconds.
gateway_addr() {
    "$SELF" __native gateway "$SERVER"
}

cmd_check() {
    local out gw
    gw=$(gateway_addr) || die "$SERVER is unreachable on port 443 (IPv4 and IPv6)"
    out=$(openconnect --authenticate --non-inter "${UA_ARGS[@]}" --resolve="$SERVER:$gw" "$SERVER" </dev/null 2>&1) || true
    # The ASA labels it "GROUP:", ocserv "Group:".
    if echo "$out" | grep -qi 'GROUP: \['; then
        echo "ok: $SERVER ($gw) offers the login form to openconnect"
        echo "$out" | grep -i 'GROUP:' | sed 's/:\*\*\*$//'
    else
        echo "$out" >&2
        die "server did not return a login form (User-Agent workaround may have stopped working)"
    fi
}

cmd_up() {
    local mode=$RWTH_MODE user=$RWTH_USER dtls=$RWTH_DTLS
    while [ $# -gt 0 ]; do
        case "$1" in
            --split) mode="split";;
            --full) mode="full";;
            --dtls) dtls=1;;
            --no-dtls) dtls=0;;
            --user) user=${2:?--user needs a value}; shift;;
            --user=*) user=${1#--user=};;
            *) die "unknown option: $1";;
        esac
        shift
    done
    is_running && die "already connected (rwth-vpn down first)"
    command -v openconnect >/dev/null || die "openconnect not installed (run install.sh)"
    find_vpnc_script >/dev/null || die "vpnc-script not found (install your distro's vpnc-scripts package)"

    local group=$GROUP_FULL
    if [ "$mode" = split ]; then
        group=$GROUP_SPLIT
        find_vpn_slice >/dev/null || die "--split needs vpn-slice (run install.sh)"
    fi

    # Ask for the root password before the TOTP so the code doesn't expire meanwhile.
    local el=""
    if ! is_root; then
        el=$(elevate_cmd)
        if [ "$el" = sudo ]; then sudo -v; else doas true; fi || die "root is required to create the tunnel"
    fi

    # A previous crash can leave DNS unusable, so recover before resolving the gateway.
    if [ -e "$RECOVERY_FILE" ]; then
        if is_root; then recover_tunnel; else "$el" "$SELF" down; fi
    fi
    local gw
    gw=$(gateway_addr)
    [ -n "$gw" ] || die "$SERVER is unreachable on port 443 (IPv4 and IPv6)"

    # Only one RWTH session at a time: the ssh proxy session would compete.
    proxy_stop

    local auth
    auth=$(authenticate "$group" "$gw" "$user") || die "authentication failed"

    local cookie url fp
    cookie=$(auth_field "$auth" COOKIE)
    url=$(auth_field "$auth" CONNECT_URL)
    fp=$(auth_field "$auth" FINGERPRINT)
    [ -n "$cookie" ] && [ -n "$fp" ] || die "unexpected output from openconnect --authenticate"
    [ -n "$url" ] || url="https://$SERVER/"

    if is_root; then
        printf '%s\n' "$cookie" | "$SELF" _connect "$mode" "$url" "$fp" "$SERVER:$gw" "$dtls"
    else
        printf '%s\n' "$cookie" | "$el" env SUDO_USER_HOME="$HOME" \
            "$SELF" _connect "$mode" "$url" "$fp" "$SERVER:$gw" "$dtls"
    fi
}

# Runs as root; reads the session cookie from stdin.
cmd__connect() {
    local mode=$1 url=$2 fp=$3 resolve=$4 dtls=${5:-$RWTH_DTLS}
    is_root || die "_connect must run as root"
    mkdir -p "$RUN_DIR"
    chmod 755 "$RUN_DIR"

    local vpnc
    vpnc=$(find_vpnc_script) || die "vpnc-script not found"
    export RWTH_VPN_MODE=$mode RWTH_VPN_VPNC_SCRIPT=$vpnc RWTH_VPN_STATE=$STATE_FILE
    if [ "$mode" = split ]; then
        RWTH_VPN_SLICE=$(find_vpn_slice) || die "vpn-slice not found"
        export RWTH_VPN_SLICE RWTH_VPN_SPLIT_ROUTES=$SPLIT_ROUTES
    fi

    # A fixed name is nice on Linux; macOS needs utunN and the BSDs tunN, so let
    # openconnect pick there.
    local args=("${UA_ARGS[@]}" --cookie-on-stdin --servercert="$fp" --resolve="$resolve"
                --script="'$SELF' _hook" --background --pid-file="$PID_FILE" --timestamp)
    [ "$(uname -s)" = Linux ] && args+=(--interface=tun-rwth)
    [ "$dtls" = 1 ] || args+=(--no-dtls)

    printf '\n=== %s up (%s)\n' "$(date)" "$mode" >>"$LOG_FILE"
    openconnect "${args[@]}" "$url" >>"$LOG_FILE" 2>&1 \
        || { tail -n 20 "$LOG_FILE" >&2; recover_tunnel; die "tunnel failed (log: $LOG_FILE)"; }
    # PID and connection metadata are public; hook recovery inputs remain 0600.
    chmod 644 "$PID_FILE"

    # openconnect backgrounds before it runs the hook; wait for the hook's state.
    local n=0
    while [ ! -r "$STATE_FILE" ] && [ $n -lt 20 ]; do sleep 0.5; n=$((n + 1)); done
    if [ ! -r "$STATE_FILE" ]; then
        cmd_down
        die "routing/DNS hook did not complete; tunnel stopped"
    fi
    cmd_status
}

# openconnect --script hook (rwth-vpn _hook). Runs as root.
#
# full:  stock vpnc-script (default route + DNS via RWTH), plus on Linux:
#        - Tailscale's underlay (fwmark 0x80000) keeps using the physical uplink,
#          so direct peers stay direct instead of detouring through Aachen;
#        - if the VPN hands out no IPv6, native IPv6 is made unreachable so
#          dual-stack sites (publishers) fall back to IPv4 and see an RWTH address.
# split: vpn-slice routes the server's split-include networks plus all RWTH ranges.
#
# Inputs from rwth-vpn: RWTH_VPN_MODE, RWTH_VPN_VPNC_SCRIPT, RWTH_VPN_STATE,
# RWTH_VPN_SLICE, RWTH_VPN_SPLIT_ROUTES. Everything else comes from openconnect.
# shellcheck disable=SC2154,SC2153  # reason, TUNDEV, INTERNAL_IP*: set by openconnect

TABLE=7337          # holds the physical default route(s) for Tailscale
PREF_MAIN=5200      # must sit before Tailscale's 5210 rule
PREF_UPLINK=5201
# IPv6 block: after Tailscale's 5270 (tailnet) rule. It lives in its own table
# because an unreachable route in main ends IPv6 rule lookup even when
# suppress_prefixlength should skip it.
V6_TABLE=7338
PREF_V6_MAIN=5280
PREF_V6_BLOCK=5281
TS_MARK=0x80000/0xff0000

write_state() {
    local tmp
    tmp=$(mktemp "$RUN_DIR/.state.XXXXXXXX") || return 1
    cat >"$tmp" <<EOF
MODE='$RWTH_VPN_MODE'
TUNDEV='$TUNDEV'
IP4='$INTERNAL_IP4_ADDRESS'
IP6='${INTERNAL_IP6_ADDRESS:-}'
SINCE='$(date '+%Y-%m-%d %H:%M')'
V6_BLOCKED='${V6_BLOCKED:-}'
EOF
    chmod 644 "$tmp" && mv "$tmp" "$RWTH_VPN_STATE"
}

# Save only the routing/DNS hook inputs, never authentication cookies or passwords.
# declare -p quotes server-supplied values safely for Bash. The file is root-only.
save_recovery() {
    local name tmp
    tmp=$(umask 077; mktemp "$RUN_DIR/.disconnect.XXXXXXXX") || return 1
    for name in $(compgen -e); do
        case "$name" in
            VPNPID|VPNGATEWAY|TUNDEV|INTERNAL_*|CISCO_*|RWTH_VPN_*)
                declare -p "$name";;
        esac
    done >"$tmp"
    mv "$tmp" "$RECOVERY_FILE"
}

recover_tunnel() {
    [ -f "$RECOVERY_FILE" ] || return 0
    is_root || die "root is required to restore tunnel routing and DNS"
    (
        # shellcheck disable=SC1090
        . "$RECOVERY_FILE"
        export reason=disconnect
        "$SELF" _hook >>"$LOG_FILE" 2>&1
    ) || die "could not restore tunnel routing/DNS; see $LOG_FILE; recovery data kept in $RUN_DIR"
    rm -f "$PID_FILE" "$STATE_FILE" "$RECOVERY_FILE"
}

has_tailscale_rules() {
    ip rule show 2>/dev/null | grep -q "fwmark $TS_MARK"
}

# Copy the current physical default route of family $1 into $TABLE and steer
# Tailscale-marked packets there, ignoring the VPN's default (/0 or /1 routes).
keep_tailscale_on_uplink() {
    fam=$1
    route=$(ip "$fam" route show default table main | head -n 1 | sed 's/ expires [^ ]*//')
    [ -n "$route" ] || return 0
    # shellcheck disable=SC2086
    ip "$fam" route replace $route table $TABLE
    ip "$fam" rule add pref $PREF_MAIN fwmark $TS_MARK lookup main suppress_prefixlength 1
    ip "$fam" rule add pref $PREF_UPLINK fwmark $TS_MARK lookup $TABLE
}

linux_full_cleanup() {
    for fam in -4 -6; do
        while ip "$fam" rule del pref $PREF_MAIN 2>/dev/null; do :; done
        while ip "$fam" rule del pref $PREF_UPLINK 2>/dev/null; do :; done
        ip "$fam" route flush table $TABLE 2>/dev/null
    done
    while ip -6 rule del pref $PREF_V6_MAIN 2>/dev/null; do :; done
    while ip -6 rule del pref $PREF_V6_BLOCK 2>/dev/null; do :; done
    ip -6 route flush table $V6_TABLE 2>/dev/null
    return 0
}

cmd__hook() {
    # vpnc-script conventions: plain sh semantics, unset variables allowed.
    set +eu +o pipefail
    if [ "$reason" = connect ]; then
        save_recovery || exit 1
    fi
    case "$RWTH_VPN_MODE" in
    split)
        case "$reason" in
            connect) write_state;;
            disconnect) ;;
        esac
        # shellcheck disable=SC2086
        "$RWTH_VPN_SLICE" -S -I $RWTH_VPN_SPLIT_ROUTES
        result=$?
        if [ "$reason" = disconnect ] && [ "$result" = 0 ]; then
            rm -f "$RWTH_VPN_STATE" "$RECOVERY_FILE"
        fi
        exit "$result"
        ;;
    esac

    # full mode
    if [ "$(uname -s)" != Linux ]; then
        case "$reason" in
            connect) "$RWTH_VPN_VPNC_SCRIPT" && write_state; exit;;
            disconnect)
                "$RWTH_VPN_VPNC_SCRIPT" || exit $?
                rm -f "$RWTH_VPN_STATE" "$RECOVERY_FILE"
                exit 0;;
        esac
        exec "$RWTH_VPN_VPNC_SCRIPT"
    fi

    case "$reason" in
    connect)
        linux_full_cleanup
        # Snapshot uplink routes before vpnc-script replaces the default.
        if has_tailscale_rules; then
            keep_tailscale_on_uplink -4
            keep_tailscale_on_uplink -6
        fi
        "$RWTH_VPN_VPNC_SCRIPT" || { rc=$?; linux_full_cleanup; exit $rc; }
        if [ -z "${INTERNAL_IP6_ADDRESS:-}" ] && ip -6 route show default | grep -q .; then
            ip -6 route add unreachable default table $V6_TABLE
            ip -6 rule add pref $PREF_V6_MAIN not fwmark $TS_MARK lookup main suppress_prefixlength 0
            ip -6 rule add pref $PREF_V6_BLOCK not fwmark $TS_MARK lookup $V6_TABLE
            V6_BLOCKED=1
        fi
        write_state
        ;;
    disconnect)
        "$RWTH_VPN_VPNC_SCRIPT"
        result=$?
        linux_full_cleanup
        [ "$result" = 0 ] || exit "$result"
        rm -f "$RWTH_VPN_STATE" "$RECOVERY_FILE"
        ;;
    *)
        exec "$RWTH_VPN_VPNC_SCRIPT"
        ;;
    esac
}

# backoff_active MARK: true if MARK was written less than BACKOFF_SECS ago.
backoff_active() {
    local t
    t=$(cat "$1" 2>/dev/null) || return 1
    [ $(( $(date +%s) - ${t:-0} )) -lt "$BACKOFF_SECS" ]
}

backoff_msg() {
    echo "rwth-vpn: automatic $1 login is paused for $((BACKOFF_SECS / 60)) min after a failed attempt" \
         "($(date -r "$2" '+%H:%M' 2>/dev/null || echo recently)), to protect your account from lockout." >&2
    echo "  Fix the secrets with 'rwth-vpn secrets' (this also resumes logins)." >&2
}

proxy_running() {
    local pid
    pid=$(cat "$PROXY_PID" 2>/dev/null) || return 1
    process_running "$pid"
}

# ssh masters ride on the VPN session; once it's gone they only hang around.
masters_stop() {
    local s
    for s in "$HOME"/.ssh/rwth-vpn-cm/*; do
        [ -S "$s" ] && ssh -o ControlPath="$s" -O exit rwth-vpn-master >/dev/null 2>&1
    done
    return 0
}

proxy_stop() {
    proxy_running || return 0
    masters_stop
    local pid n=0
    pid=$(cat "$PROXY_PID")
    kill -INT "$pid"
    while proxy_running && [ $n -lt 20 ]; do sleep 0.5; n=$((n + 1)); done
    proxy_running && kill -TERM "$pid"
    rm -f "$PROXY_PID"
}

port_open() {
    "$SELF" __native port-open "$1" 2>/dev/null
}

# Start the rootless session unless it's already up. Non-interactive when
# secrets are stored; serialised so parallel ssh calls (agents) share one.
proxy_ensure() {
    proxy_running && port_open "$RWTH_SOCKS_PORT" && return 0
    local ocproxy gw lock=$USER_DIR/proxy.lock
    ocproxy=$(find_ocproxy) || die "ocproxy not installed (run install.sh)"
    mkdir -p "$USER_DIR" && chmod 700 "$USER_DIR"

    local n=0
    until mkdir "$lock" 2>/dev/null; do
        # Another process is connecting; wait for it, but break stale locks.
        [ $n -ge 120 ] && { rm -rf "$lock"; continue; }
        sleep 0.5; n=$((n + 1))
        proxy_running && port_open "$RWTH_SOCKS_PORT" && return 0
    done
    # shellcheck disable=SC2064
    trap "rm -rf '$lock'" EXIT

    proxy_stop
    if backoff_active "$VPN_FAIL_MARK"; then
        backoff_msg VPN "$VPN_FAIL_MARK"
        exit 1
    fi
    gw=$(gateway_addr)
    [ -n "$gw" ] || die "$SERVER is unreachable on port 443 (IPv4 and IPv6)"
    local auth cookie url fp
    printf '\n=== %s proxy up\n' "$(date)" >>"$PROXY_LOG"
    auth=$(AUTH_LOG=$PROXY_LOG authenticate "$GROUP_FULL" "$gw" </dev/null) || auth=""
    cookie=$(auth_field "$auth" COOKIE)
    url=$(auth_field "$auth" CONNECT_URL)
    fp=$(auth_field "$auth" FINGERPRINT)
    if [ -z "$cookie" ]; then
        date +%s >"$VPN_FAIL_MARK"
        die "VPN login failed (see $PROXY_LOG); check with 'rwth-vpn test-login'"
    fi
    rm -f "$VPN_FAIL_MARK"

    local transport_args=("${UA_ARGS[@]}")
    [ "$RWTH_DTLS" = 1 ] || transport_args+=(--no-dtls)
    printf '%s\n' "$cookie" | openconnect "${transport_args[@]}" --cookie-on-stdin \
        --servercert="$fp" --resolve="$SERVER:$gw" \
        --script-tun --script="$ocproxy -D $RWTH_SOCKS_PORT -k 60" \
        --background --pid-file="$PROXY_PID" --timestamp \
        "${url:-https://$SERVER/}" >>"$PROXY_LOG" 2>&1 \
        || die "VPN tunnel failed (see $PROXY_LOG)"
    n=0
    until port_open "$RWTH_SOCKS_PORT"; do
        [ $n -ge 40 ] && die "SOCKS proxy did not come up (see $PROXY_LOG)"
        sleep 0.25; n=$((n + 1))
    done
}

# ssh ProxyCommand: use the system tunnel if it's up, else the proxy session.
cmd__ssh_proxy() {
    local host=$1 port=$2
    if is_running; then
        exec "$SELF" __native connect "$host" "$port"
    fi
    # Subshell: its EXIT trap must drop the lock before exec replaces us.
    (proxy_ensure) >&2
    exec "$SELF" __native connect "$host" "$port" "$RWTH_SOCKS_PORT"
}

# ssh `Match exec` hook: make sure a logged-in master connection exists for
# this host, so the actual ssh (yours or an agent's) needs no prompts at all.
# Always succeeds: on failure, ssh just falls back to normal interactive auth.
cmd__ssh_master() {
    local host=$1
    # Our own ssh calls below re-read the config; don't recurse.
    [ -n "${RWTH_VPN_SSH_INNER:-}" ] && return 0
    export RWTH_VPN_SSH_INNER=1
    local marker=$USER_DIR/master.$(printf '%s' "$host" | tr -c 'A-Za-z0-9.-' _).session
    if ssh -O check "$host" >/dev/null 2>&1; then
        "$SELF" __native ssh-session check "$marker" "$PID_FILE" "$STATE_FILE" "$PROXY_PID" >/dev/null 2>&1 && return 0
        # The transport under this master ended or was replaced: reconnect.
        ssh -O exit "$host" >/dev/null 2>&1 || true
    fi
    have_secret hpc_password || return 0
    # ssh discards the ProxyCommand's stderr when ControlMaster is set, so
    # VPN login trouble is reported from here.
    if ! is_running && backoff_active "$VPN_FAIL_MARK"; then
        backoff_msg VPN "$VPN_FAIL_MARK"
        return 0
    fi
    # Paused after a failed login: skip, so a human gets normal prompts and an
    # agent (BatchMode) fails without another attempt against the account.
    if backoff_active "$HPC_FAIL_MARK"; then
        backoff_msg HPC "$HPC_FAIL_MARK"
        return 0
    fi

    mkdir -p "$USER_DIR" && chmod 700 "$USER_DIR"
    local lock n=0
    lock=$USER_DIR/master.$(printf '%s' "$host" | tr -c 'A-Za-z0-9.-' _).lock
    until mkdir "$lock" 2>/dev/null; do
        [ $n -ge 120 ] && { rm -rf "$lock"; continue; }
        sleep 0.5; n=$((n + 1))
        ssh -O check "$host" >/dev/null 2>&1 && return 0
    done
    # shellcheck disable=SC2064
    trap "rm -rf '$lock'" EXIT
    ssh -O check "$host" >/dev/null 2>&1 && return 0

    # SSH_ASKPASS_REQUIRE=force (OpenSSH >= 8.4) routes every prompt
    # (password, TOTP) to our helper even with a terminal attached. One try:
    # repeating the same stored answers only counts towards a lockout.
    # Output goes to a file, not $(...): the backgrounded master (-f) would
    # hold a pipe open for its whole lifetime.
    local out tmp=$USER_DIR/master.out.$$
    echo "rwth-vpn: connecting to $host (may wait for an unused 2FA code)..." >&2
    RWTH_VPN_ASKPASS=1 SSH_ASKPASS="$SELF" SSH_ASKPASS_REQUIRE=force DISPLAY=${DISPLAY:-:0} \
        ssh -f -N -M -o BatchMode=no -o NumberOfPasswordPrompts=1 \
            -o ServerAliveInterval=15 -o ServerAliveCountMax=2 -o ConnectTimeout=90 "$host" \
        </dev/null >"$tmp" 2>&1 || true
    out=$(cat "$tmp"); rm -f "$tmp"
    printf '=== %s master %s\n%s\n' "$(date)" "$host" "$out" >>"$USER_DIR/ssh.log"
    if ssh -O check "$host" >/dev/null 2>&1; then
        "$SELF" __native ssh-session record "$marker" "$PID_FILE" "$STATE_FILE" "$PROXY_PID" >/dev/null 2>&1 || true
        rm -f "$HPC_FAIL_MARK"
    elif printf '%s' "$out" | grep -q 'Permission denied'; then
        # Only a rejected login pauses retries; network trouble doesn't.
        date +%s >"$HPC_FAIL_MARK"
        echo "rwth-vpn: HPC login rejected (see $USER_DIR/ssh.log); check 'rwth-vpn secrets'" >&2
    elif ! backoff_active "$VPN_FAIL_MARK"; then
        echo "rwth-vpn: could not reach $host (see $USER_DIR/ssh.log)" >&2
    else
        echo "rwth-vpn: VPN login failed (see $PROXY_LOG); check with 'rwth-vpn test-login'" >&2
    fi
    return 0
}

# SSH_ASKPASS (this script, with RWTH_VPN_ASKPASS=1): answers the HPC login prompts from stored secrets.
# ssh passes the prompt text as $1; we print the answer, or fail on prompts we
# don't recognise so ssh stops instead of sending something wrong.


cmd__askpass() {
    # OpenSSH prefixes "(user@host) "; drop it so a user or host name containing
    # "otp"/"token" can't change which answer we give.
    local prompt
    prompt=$(printf '%s' "$1" | sed 's/^([^)]*) *//' | tr '[:upper:]' '[:lower:]')

    case "$prompt" in
        # CLAIX asks "Two-factor code:" (before the password).
        *two-factor*|*two\ factor*|*otp*|*token*|*one-time*|*one\ time*|*second\ factor*|*2fa*|*verification\ code*|*authenticator*)
            # The HPC has its own TOTP tokens (RegApp), separate from the VPN's.
            secret_get hpc_totp_secret | "$SELF" __native totp --fresh
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
}

cmd_ssh_setup() {
    local hpc_user=${RWTH_HPC_USER:-$(secret_get hpc_user || true)}
    hpc_user=${hpc_user:-$RWTH_USER}
    mkdir -p "$HOME/.ssh/rwth-vpn-cm" && chmod 700 "$HOME/.ssh" "$HOME/.ssh/rwth-vpn-cm"
    cat >"$SSH_CONF" <<EOF
# Generated by 'rwth-vpn ssh-setup'. RWTH HPC (CLAIX) through the RWTH VPN,
# logging in with stored secrets so plain ssh/scp/rsync need no prompts.

Host claix
    HostName login23-1.hpc.itc.rwth-aachen.de
Host login23-* copy23-*
    HostName %h.hpc.itc.rwth-aachen.de

Match originalhost "claix,login23-*,copy23-*,*.hpc.itc.rwth-aachen.de" exec "'$SELF' _ssh-master %n"
${hpc_user:+    User $hpc_user
}    ProxyCommand '$SELF' _ssh-proxy %h %p
    ControlMaster auto
    ControlPath ~/.ssh/rwth-vpn-cm/%C
    ControlPersist 8h
    ServerAliveInterval 60
    StrictHostKeyChecking accept-new
EOF
    local cfg=$HOME/.ssh/config line="Include $SSH_CONF"
    touch "$cfg"
    if ! grep -qxF "$line" "$cfg"; then
        # Include must come before any Host block to apply to every host.
        { echo "$line"; echo; cat "$cfg"; } >"$cfg.rwth-vpn.tmp" && mv "$cfg.rwth-vpn.tmp" "$cfg"
        chmod 600 "$cfg"
        echo "added '$line' to the top of $cfg"
    fi
    echo "wrote $SSH_CONF"
}

IDM_URL=https://idm.rwth-aachen.de/selfservice/
REGAPP_URL=https://regapp.itc.rwth-aachen.de/
VPN_HELP_URL=https://help.itc.rwth-aachen.de/en/service/vbf6fx0gom76/article/34c748946f63455090ede2dd5a7a49e5
HPC_HELP_URL=https://help.itc.rwth-aachen.de/service/rhr4fjjutttf/article/fb61d6c86ae245b5b7bba8c0cb7db6eb/

secrets_guide_vpn() {
    cat <<EOF
VPN, needed for everything. Both come from IdM Selfservice:
  $IDM_URL
  - VPN password: set it there. It is NOT your RWTH single sign-on password.
  - TOTP secret: Token Manager -> new token -> "Authenticator app (TOTP)".
    Copy the secret text shown with the QR code (or the otpauth:// link).
    Also scan the QR code with your phone, so you can still log in by hand.
    Keep SHA1 / 6 digits, or paste the otpauth:// link, which carries them.
  Guide: $VPN_HELP_URL
EOF
}

secrets_guide_hpc() {
    cat <<EOF
HPC (CLAIX), only for ssh. The HPC has its own password and its own TOTP
tokens, both in RegApp:
  $REGAPP_URL
  - HPC password: your HPC account's password (not the VPN one).
  - HPC TOTP secret: set up a TOTP second factor there and copy its secret
    the same way as above.
  Guide: $HPC_HELP_URL
EOF
}

# prompt_secret NAME LABEL HIDDEN [DEFAULT_NOTE]
unlock_secrets() {
    [ -t 0 ] || die "unlock requires an interactive terminal; run 'rwth-vpn secrets --unlock' over SSH"
    case "$(secret_backend)" in
        file) echo "Unencrypted storage needs no unlocking."; return;;
        keychain)
            echo "Unlock your login Keychain (enter its password at the security prompt)." >&2
            security unlock-keychain "$HOME/Library/Keychains/login.keychain-db" ||
                die "could not unlock login Keychain"
            ;;
        secret-service)
            command -v gnome-keyring-daemon >/dev/null 2>&1 ||
                die "unlock your Secret Service provider, then retry; automatic terminal unlocking currently supports GNOME Keyring"
            local password
            printf 'Login keyring password (hidden): ' >&2
            IFS= read -r -s password; printf '\n' >&2
            [ -n "$password" ] || die "a nonempty keyring password is required for encrypted storage"
            printf '%s' "$password" | gnome-keyring-daemon --unlock --components=secrets >/dev/null ||
                die "could not unlock GNOME Keyring"
            unset password
            ;;
    esac
    # Verify actual access, not just that the unlock command returned.
    storage_probe
}

storage_probe() {
    [ "$(secret_backend)" != file ] || return 0
    echo "Accessing encrypted storage. Approve any keyring prompt; if locked or unavailable, run 'rwth-vpn secrets --unlock'. Headless Linux also needs a running session D-Bus and Secret Service provider." >&2
    if secret_set storage_check ready && [ "$(secret_get storage_check)" = ready ]; then return; fi
    if [ -t 0 ]; then
        local answer
        printf 'Keyring unavailable. Try unlocking it in this terminal? [y/N] ' >&2
        IFS= read -r answer || answer=n
        case "$answer" in [Yy]*) unlock_secrets; return;; esac
    fi
    die "encrypted storage unavailable; unlock the keyring and retry (no file fallback)"
}

select_storage() {
    local mode=$1 old name value tmp
    case "$mode" in encrypted|unencrypted) ;; *) die "mode must be encrypted or unencrypted";; esac
    old=$(secret_backend)
    # Ensure the source keyring is available before treating missing keys as unset.
    storage_probe
    SECRET_MODE_OVERRIDE=$mode storage_probe
    # Verify every copied value before committing the new mode or deleting originals.
    if [ "$old" != "$(SECRET_MODE_OVERRIDE=$mode secret_backend)" ]; then
        for name in $SECRET_NAMES; do
            if value=$(secret_get "$name"); then :
            else
                local status=$?
                [ "$status" = 1 ] || storage_transfer_failed "cannot read original credentials"
            fi
            SECRET_MODE_OVERRIDE=$mode secret_set "$name" "$value" || storage_transfer_failed "credential transfer failed"
            local copied
            if copied=$(SECRET_MODE_OVERRIDE=$mode secret_get "$name"); then :
            else
                local status=$?
                [ "$status" = 1 ] && [ -z "$value" ] || storage_transfer_failed "credential verification failed"
            fi
            [ "$copied" = "$value" ] || storage_transfer_failed "credential verification failed"
        done
    fi
    (
        umask 077
        mkdir -p "$(dirname "$STORAGE_FILE")"
        tmp=$(mktemp "$STORAGE_FILE.XXXXXXXX")
        printf '%s\n' "$mode" >"$tmp"
        mv "$tmp" "$STORAGE_FILE"
    )
    if [ "$old" != "$(secret_backend)" ]; then
        case "$old" in
            file) rm -f "$SECRETS_FILE";;
            secret-service)
                "$SELF" __native secret-purge secret-service || die "mode changed, but old keyring credentials could not be removed";;
            keychain)
                "$SELF" __native secret-purge keychain || die "mode changed, but old Keychain credentials could not be removed";;
        esac
    fi
    echo "Credential storage: $mode ($(secret_backend))."
}

storage_transfer_failed() {
    # select_storage's local mode is visible here. The destination is inactive;
    # remove partial copies while keeping the original backend and mode intact.
    local target
    target=$(SECRET_MODE_OVERRIDE=$mode secret_backend)
    if [ "$target" = file ]; then rm -f "$SECRETS_FILE"
    else
        "$SELF" __native secret-purge "$target" ||
            echo "rwth-vpn: could not remove partial copies from $target; unlock it and retry the mode change" >&2
    fi
    die "$1; original mode and credentials kept"
}

prompt_secret() {
    local name=$1 label=$2 hidden=$3 note=${4:-unset} cur v
    cur=$(secret_get "$name" || true)
    while :; do
        if [ "$hidden" = 1 ]; then
            printf '  %s [%s]: ' "$label" "$([ -n "$cur" ] && echo set || echo "$note")"
            stty -echo; IFS= read -r v || true; stty echo; echo
        else
            printf '  %s [%s]: ' "$label" "${cur:-$note}"
            IFS= read -r v || true
        fi
        case "$v" in
            "") return 0;;
            -) secret_set "$name" ""; echo "    deleted"; return 0;;
        esac
        case "$name" in
            *totp_secret)
                if ! printf '%s' "$v" | "$SELF" __native totp >/dev/null 2>&1; then
                    echo "    That isn't a base32 secret or otpauth:// link; try again ('-' to delete)."
                    continue
                fi
                secret_set "$name" "$v"
                echo "    Current code: $(printf '%s' "$v" | "$SELF" __native totp)  <- should match your phone"
                ;;
            *) secret_set "$name" "$v";;
        esac
        return 0
    done
}

cmd_secrets() {
    local mode="" want_hpc="" unlock=0 profile=""
    while [ $# -gt 0 ]; do
    case "$1" in
        --no-hpc) want_hpc=0;;
        --hpc) want_hpc=1;;
        --unlock) unlock=1;;
        --mode)
            [ $# -ge 2 ] || die "--mode needs encrypted or unencrypted"
            mode=$2
            case "$mode" in encrypted|unencrypted) ;; *) die "mode must be encrypted or unencrypted";; esac
            shift;;
        --where|--help|-h)
            secrets_guide_vpn; echo; secrets_guide_hpc
            echo; echo "Storage: rwth-vpn secrets --mode encrypted|unencrypted"
            echo "Unlock: rwth-vpn secrets --unlock"
            echo; echo "Run 'rwth-vpn secrets' in a terminal to enter them."
            return 0;;
        *) die "usage: rwth-vpn secrets [--mode encrypted|unencrypted] [--unlock] [--no-hpc|--hpc]";;
    esac
    shift
    done
    if [ "$unlock" = 1 ]; then
        if [ -n "$mode" ]; then SECRET_MODE_OVERRIDE=$mode unlock_secrets
        else unlock_secrets; fi
        return
    fi
    [ -t 0 ] || { secrets_guide_vpn; echo; secrets_guide_hpc; die "run 'rwth-vpn secrets' in a terminal to enter them"; }

    if [ -z "$mode" ] && [ ! -f "$STORAGE_FILE" ]; then
        echo "Choose storage: encrypted (Keychain / Secret Service) or unencrypted (0600 file)."
        printf 'Storage mode [encrypted]: '
        IFS= read -r mode
        mode=${mode:-encrypted}
    fi
    if [ -n "$mode" ]; then select_storage "$mode"; else storage_probe; fi
    if [ -z "$want_hpc" ]; then
        if [ -f "$PROFILE_FILE" ]; then IFS= read -r profile <"$PROFILE_FILE" || true; fi
        case "$profile" in
            vpn-only) want_hpc=0;;
            hpc) want_hpc=1;;
            *)
                printf 'Set up HPC credentials and SSH access too? [y/N] '
                local answer
                IFS= read -r answer || answer=n
                case "$answer" in [Yy]*) want_hpc=1;; *) want_hpc=0;; esac;;
        esac
    fi
    if [ "$want_hpc" = 1 ]; then echo hpc >"$PROFILE_FILE"
    else echo vpn-only >"$PROFILE_FILE"; fi
    case "$(secret_backend)" in
        keychain) echo "Secrets are stored in your macOS login Keychain (service rwth-vpn).";;
        secret-service) echo "Secrets are stored in Linux Secret Service (service rwth-vpn).";;
        file) echo "Secrets are stored in $SECRETS_FILE (mode 0600, base64 encoded, NOT encrypted).";;
    esac
    load_config
    tls_workaround
    echo "Enter keeps the current value, '-' deletes it. Typing is hidden for passwords and secrets."
    echo
    secrets_guide_vpn
    prompt_secret user "RWTH username, e.g. ab123456" 0
    prompt_secret vpn_password "VPN password" 1
    prompt_secret totp_secret "VPN TOTP secret" 1
    echo
    if [ "$want_hpc" = 1 ]; then
        secrets_guide_hpc
        prompt_secret hpc_user "HPC username" 0 "same as RWTH"
        prompt_secret hpc_password "HPC password" 1
        prompt_secret hpc_totp_secret "HPC TOTP secret" 1
    fi

    echo
    if have_secret user && have_secret vpn_password && have_secret totp_secret; then
        printf 'Test the VPN login now? [Y/n] '
        local ans; read -r ans || ans=n
        case "$ans" in
            [Nn]*) ;;
            *) cmd_test_login;;
        esac
    else
        echo "VPN secrets incomplete: logins will prompt for what's missing."
    fi
    rm -f "$VPN_FAIL_MARK" "$HPC_FAIL_MARK"
    # Regenerate: the HPC username goes into the ssh config's User line.
    if [ "$want_hpc" = 1 ]; then
        cmd_ssh_setup >/dev/null
        if have_secret hpc_password; then echo "Next: ssh claix hostname"; fi
    fi
}

# Log in without starting a tunnel, to check the stored secrets.
cmd_test_login() {
    local gw out
    gw=$(gateway_addr)
    [ -n "$gw" ] || die "$SERVER is unreachable on port 443 (IPv4 and IPv6)"
    if out=$(AUTH_LOG=/dev/stderr authenticate "$GROUP_FULL" "$gw" </dev/null 2>&1) &&
        [ -n "$(auth_field "$out" COOKIE)" ]; then
        rm -f "$VPN_FAIL_MARK"
        echo "ok: VPN login works"
    else
        printf '%s\n' "$out" | grep -v '^COOKIE=' | tail -n 8 >&2
        die "VPN login failed: check username, VPN password and TOTP secret ('rwth-vpn secrets')"
    fi
}

cmd_totp() {
    local name=totp_secret
    [ "${1:-}" = hpc ] && name=hpc_totp_secret
    have_secret "$name" || die "no ${1:+HPC }TOTP secret stored (run 'rwth-vpn secrets')"
    secret_get "$name" | "$SELF" __native totp
}

cmd_down() {
    proxy_running && { proxy_stop; echo "ssh proxy session stopped"; }
    # A restrictive root umask on older installations can hide the PID file.
    # Its existence still means root must inspect and clean up the session.
    if ! is_root && { [ -e "$PID_FILE" ] || [ -e "$RECOVERY_FILE" ]; }; then
        exec "$(elevate_cmd)" "$SELF" down
    fi
    if ! is_running; then
        if [ -e "$RECOVERY_FILE" ]; then
            is_root || exec "$(elevate_cmd)" "$SELF" down
            recover_tunnel
            echo "restored routing and DNS from the exited VPN session"
        elif [ -e "$STATE_FILE" ]; then
            die "stale tunnel state without recovery data; reconnect your normal network to restore routing/DNS"
        else
            echo "not connected"
        fi
        return 0
    fi
    is_root || exec "$(elevate_cmd)" "$SELF" down
    local pid n=0
    pid=$(cat "$PID_FILE")
    # SIGINT makes openconnect log off the session and run the disconnect hook.
    kill -INT "$pid"
    while is_running && [ $n -lt 30 ]; do
        sleep 0.5
        n=$((n + 1))
    done
    is_running && { kill -TERM "$pid"; sleep 1; }
    is_running && die "VPN process did not stop; recovery data kept in $RUN_DIR"
    recover_tunnel
    rm -f "$PID_FILE"
    echo "disconnected"
}

cmd_uninstall() {
    local purge=0 cfg="$HOME/.ssh/config" tmp socket name
    case "$#:${1:-}" in
        0:) ;;
        1:--purge) purge=1;;
        *) die "usage: rwth-vpn uninstall [--purge]";;
    esac
    if is_running || [ -e "$RECOVERY_FILE" ]; then
        die "run 'rwth-vpn down' before uninstalling the whole-machine tunnel"
    fi
    # Close persistent SSH masters before removing their proxy configuration.
    for socket in "$HOME/.ssh/rwth-vpn-cm/"*; do
        [ -S "$socket" ] || continue
        ssh -F /dev/null -S "$socket" -O exit claix ||
            die "could not stop SSH session at $socket; close it before uninstalling"
    done
    if proxy_running; then proxy_stop; fi
    if [ -f "$cfg" ]; then
        tmp=$(mktemp "$cfg.rwth-vpn.XXXXXXXX")
        # Remove only the exact Include line written by ssh-setup.
        if ! awk -v line="Include $SSH_CONF" '$0 != line' "$cfg" >"$tmp"; then
            rm -f "$tmp"
            die "could not update $cfg"
        fi
        chmod 600 "$tmp"
        cat "$tmp" >"$cfg"
        rm -f "$tmp"
    fi
    rm -f "$SSH_CONF"
    rmdir "$HOME/.ssh/rwth-vpn-cm" 2>/dev/null || true
    if [ "$purge" = 1 ]; then
        if [ "$(secret_backend)" = secret-service ]; then
            storage_probe
            "$SELF" __native secret-purge secret-service || die "could not delete keyring credentials; unlock the keyring and retry"
        fi
        if [ "$(secret_backend)" = keychain ]; then
            "$SELF" __native secret-purge keychain || die "could not delete Keychain credentials; unlock and retry"
        fi
        rm -f "$SECRETS_FILE" "$SECRETS_FILE.lock" "$STORAGE_FILE" "$PROFILE_FILE" "${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/config"
        rmdir "$(dirname "$SECRETS_FILE")" 2>/dev/null || true
        echo "Credentials and settings deleted."
    else
        echo "Credentials and settings kept; use uninstall --purge to delete them before uninstalling."
    fi
    rm -f "$SELF"
    echo "rwth-vpn uninstalled. Dependencies were kept."
}

cmd_status() {
    if proxy_running; then
        echo "ssh proxy session: up (SOCKS5 127.0.0.1:$RWTH_SOCKS_PORT, pid $(cat "$PROXY_PID"))"
    fi
    if ! is_running; then
        if [ -e "$RECOVERY_FILE" ]; then
            echo "rwth-vpn: VPN process exited; routing/DNS cleanup pending (run 'rwth-vpn down')"
            return 1
        fi
        echo "rwth-vpn: not connected"
        return 1
    fi
    local MODE="" TUNDEV="" IP4="" IP6="" SINCE="" V6_BLOCKED=""
    # shellcheck disable=SC1090
    [ -r "$STATE_FILE" ] && . "$STATE_FILE"
    echo "rwth-vpn: connected (${MODE:-?} tunnel) since ${SINCE:-?}"
    echo "  interface: ${TUNDEV:-?}  address: ${IP4:-?}${IP6:+  $IP6}"
    if [ "${V6_BLOCKED:-}" = 1 ]; then
        echo "  native IPv6 blocked while connected (VPN gave no IPv6)"
    fi
}

usage() {
    "$SELF" __native help
}

main() {
    # ssh calling us as SSH_ASKPASS: one argument, the prompt text. (Children of
    # that ssh inherit the variable, but always pass a subcommand plus args.)
    if [ -n "${RWTH_VPN_ASKPASS:-}" ] && [ $# -eq 1 ]; then
        case "$1" in _*|-*|up|down|status|check|secrets|test-login|totp|ssh-setup|uninstall|help) ;;
            *) cmd__askpass "$1"; return;;
        esac
    fi
    [ "${1:-}" = _hook ] && { cmd__hook; return; }
    local cmd=${1:-}
    [ $# -gt 0 ] && shift
    # Uninstall also works when dependencies or the user config are broken.
    [ "$cmd" = uninstall ] && { cmd_uninstall "$@"; return; }
    # Storage setup/unlock must happen before load_config attempts keyring reads.
    [ "$cmd" = secrets ] && { cmd_secrets "$@"; return; }
    case "$cmd" in
        -h|--help|help|"") usage; return;;
        up|test-login|ssh-setup|_ssh-proxy|_ssh-master) load_config;;
        *) load_config --no-secrets;;
    esac
    tls_workaround
    case "$cmd" in
        up) cmd_up "$@";;
        down) cmd_down;;
        status) cmd_status;;
        check) cmd_check;;
        secrets) cmd_secrets "$@";;
        test-login) cmd_test_login;;
        totp) cmd_totp "$@";;
        ssh-setup) cmd_ssh_setup;;
        _connect) cmd__connect "$@";;
        _ssh-proxy) cmd__ssh_proxy "$@";;
        _ssh-master) cmd__ssh_master "$@";;
        # For the test lab: secrets by name (value on stdin for set), TOTP of stdin.
        _secret-get) secret_get "$1";;
        _secret-set) secret_set "$1" "$(cat)";;
        _totp) "$SELF" __native totp "$@";;
        -h|--help|help|"") usage;;
        *) usage; exit 2;;
    esac
}

main "$@"
