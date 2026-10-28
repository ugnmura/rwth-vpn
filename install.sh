#!/bin/sh
# Install rwth-vpn for the current user. Never uses sudo: everything goes into
# your home directory, and missing system packages are listed with the one
# command to install them. Only `rwth-vpn up` (whole-machine tunnel) needs
# root, when you run it.
# POSIX sh on purpose: Void, Alpine and the BSDs don't ship bash by default.
#
#   ./install.sh [--with-split] [--hpc] [--user ab123456]

set -eu

DEFAULT_VERSION=latest
REPO=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
SOURCE=$REPO/dist/rwth-vpn
DOWNLOAD_DIR=""
# Download only when the installer is used without a checkout.
trap '[ -z "$DOWNLOAD_DIR" ] || rm -rf "$DOWNLOAD_DIR"' 0
trap 'exit 1' HUP INT TERM
WANT_SPLIT=0
WANT_HPC=0
HPC_CHOICE=0
RWTH_USER=""

while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help)
            cat <<'EOF'
Usage: sh install.sh [options]

Install the release binary for your platform; VPN-only is the default.

  --hpc          Set up optional CLAIX SSH access and dependencies
  --no-hpc       Skip HPC setup and its interactive question
  --with-split   Optionally install vpn-slice (needs Python via uv/pipx)
  --no-split     Skip vpn-slice installation (default)
  --user NAME    Preconfigure your RWTH username
  -h, --help     Show this help

The installer never uses sudo. Missing system packages are listed for you to
install. The binary needs Bash and openconnect plus system VPN routing tools.
RWTH_VPN_VERSION overrides the release version. Run again to update.
After installing, run rwth-vpn --help for usage and credential setup.
EOF
            exit 0;;
        --no-split) WANT_SPLIT=0;;
        --with-split) WANT_SPLIT=1;;
        --no-hpc) WANT_HPC=0; HPC_CHOICE=1;;
        --hpc) WANT_HPC=1; HPC_CHOICE=1;;
        --user) RWTH_USER=${2:?--user needs a value}; shift;;
        *) echo "unknown option: $1" >&2; exit 2;;
    esac
    shift
done

say() { printf '\n==> %s\n' "$*"; }
warn() { echo "warning: $*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

if [ "$HPC_CHOICE" = 0 ] && [ -t 0 ] && [ -t 1 ]; then
    printf 'Set up HPC access (CLAIX over SSH) too? [y/N] '
    read -r ans || ans=n
    case "$ans" in [Yy]*) WANT_HPC=1;; esac
fi

OS=$(uname -s)
PM=""
for c in brew port pacman apt-get dnf yum zypper xbps-install apk emerge eopkg pkgin pkg_add pkg nix-env; do
    if have "$c"; then PM=$c; break; fi
done

have_vpnc_script() {
    for p in /etc/vpnc/vpnc-script /usr/share/vpnc-scripts/vpnc-script \
             /usr/libexec/vpnc-scripts/vpnc-script /usr/local/etc/vpnc/vpnc-script \
             /usr/local/sbin/vpnc-script /opt/homebrew/etc/vpnc/vpnc-script \
             /opt/local/etc/vpnc/vpnc-script /usr/pkg/etc/vpnc/vpnc-script; do
        [ -x "$p" ] && return 0
    done
    have openconnect || return 1
    p=$(openconnect --help 2>&1 | sed -n 's/^ *default: "\(.*vpnc-script[^"]*\)".*/\1/p' | head -n 1)
    [ -n "$p" ] && [ -x "$p" ]
}

have_libevent() {
    { have pkg-config && pkg-config --exists libevent; } ||
        [ -e /usr/include/event2/event.h ] || [ -e /usr/local/include/event2/event.h ] ||
        { have brew && [ -e "$(brew --prefix)/include/event2/event.h" ]; }
}

# Package name(s) of a dependency for this package manager ("" = not packaged
# separately).
pkg() {
    case "$1:$PM" in
        openconnect:emerge) echo net-vpn/openconnect;;
        openssh:brew) echo "";;    # macOS ships ssh
        openssh:apt-get|openssh:apk) echo openssh-client;;
        openssh:dnf|openssh:yum|openssh:zypper) echo openssh-clients;;
        vpnc-script:apt-get) echo vpnc-scripts;;
        vpnc-script:dnf|vpnc-script:yum) echo vpnc-script;;
        vpnc-script:xbps-install|vpnc-script:apk|vpnc-script:zypper) echo vpnc;;
        vpnc-script:*) echo "";;   # ships with openconnect
        ip:dnf|ip:yum) echo iproute;;
        ip:emerge) echo sys-apps/iproute2;;
        ip:*) echo iproute2;;
        ocproxy:apt-get|ocproxy:dnf|ocproxy:yum|ocproxy:brew|ocproxy:port|ocproxy:pkg) echo ocproxy;;
        ocproxy:*) echo "";;
        build:pacman) echo "base-devel autoconf automake libevent";;
        build:apt-get) echo "build-essential autoconf automake libevent-dev";;
        build:dnf|build:yum|build:zypper) echo "gcc make autoconf automake libevent-devel";;
        build:xbps-install) echo "base-devel autoconf automake libevent-devel";;
        build:apk) echo "build-base autoconf automake libevent-dev";;
        build:*) echo "autoconf automake libevent";;
        *) echo "$1";;
    esac
}

install_hint() {
    case "$PM" in
        brew) echo "brew install $*";;
        port) echo "sudo port install $*";;
        pacman) echo "sudo pacman -S --needed $*";;
        apt-get) echo "sudo apt install $*";;
        dnf|yum) echo "sudo $PM install $*";;
        zypper) echo "sudo zypper install $*";;
        xbps-install) echo "sudo xbps-install -S $*";;
        apk) echo "doas apk add $*";;
        emerge) echo "sudo emerge --noreplace $*";;
        eopkg) echo "sudo eopkg install $*";;
        pkgin) echo "sudo pkgin install $*";;
        pkg_add) echo "doas pkg_add $*";;
        pkg) echo "sudo pkg install $*";;
        nix-env) echo "add to environment.systemPackages: $*";;
        *) echo "install with your package manager: $*";;
    esac
}

say "Checking dependencies"
NEED=""      # needed for everything (ssh included)
NEED_UP=""   # only needed for `rwth-vpn up`
add() { NEED="$NEED $(pkg "$1")"; }

have openconnect || add openconnect
[ "$WANT_HPC" = 0 ] || have ssh || add openssh
have bash || add bash
have_vpnc_script || NEED_UP="$NEED_UP $(pkg vpnc-script)"
[ "$OS" != Linux ] || have ip || NEED_UP="$NEED_UP $(pkg ip)"

OCPROXY=""
if [ "$WANT_HPC" = 0 ]; then :
elif have ocproxy; then OCPROXY=$(command -v ocproxy)
elif [ -x "$HOME/.local/bin/ocproxy" ]; then OCPROXY=$HOME/.local/bin/ocproxy
elif [ -n "$(pkg ocproxy)" ]; then add ocproxy
elif ! { have cc && have make && have autoconf && have automake && have_libevent; }; then
    NEED="$NEED $(pkg build)"
fi
[ "$WANT_HPC" = 0 ] || have git || [ -n "$OCPROXY" ] || [ -n "$(pkg ocproxy)" ] || add git

# VPN-only installs need whole-machine routing; HPC installs can still use
# their rootless SSH session when these packages are unavailable.
if [ "$WANT_HPC" = 0 ]; then NEED="$NEED $NEED_UP"; NEED_UP=""; fi

# Word-split on purpose to trim: these are space-separated package lists.
# shellcheck disable=SC2086,SC2116
NEED=$(echo $NEED) NEED_UP=$(echo $NEED_UP)

# Homebrew installs per user, so run it; everything else needs root, which
# this script doesn't use.
if [ "$PM" = brew ] && [ -n "$NEED$NEED_UP" ]; then
    # shellcheck disable=SC2086
    brew install $NEED $NEED_UP
    NEED="" NEED_UP=""
    have ocproxy && OCPROXY=$(command -v ocproxy)
fi

if [ -n "$NEED" ]; then
    echo "Missing system packages. Run this once, then rerun ./install.sh:"
    # shellcheck disable=SC2086
    echo "    $(install_hint $NEED $NEED_UP)"
    exit 1
fi
if [ -n "$NEED_UP" ]; then
    warn "'rwth-vpn up' (whole-machine tunnel) also needs: $NEED_UP"
    # shellcheck disable=SC2086
    echo "    $(install_hint $NEED_UP)"
fi
echo "ok: $(openconnect --version | head -n 1)"

if [ ! -f "$SOURCE" ]; then
    say "Downloading rwth-vpn"
    DOWNLOAD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/rwth-vpn-install.XXXXXXXX")
    SOURCE=$DOWNLOAD_DIR/rwth-vpn
    case "$OS" in
        Linux) platform=linux;; Darwin) platform=darwin;;
        FreeBSD) platform=freebsd;; OpenBSD) platform=openbsd;; NetBSD) platform=netbsd;;
        *) echo "No release binary for $OS" >&2; exit 1;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;;
        *) echo "No release binary for $(uname -m)" >&2; exit 1;;
    esac
    asset=rwth-vpn_${platform}_${arch}
    release=${RWTH_VPN_VERSION:-$DEFAULT_VERSION}
    base=https://github.com/ugnmura/rwth-vpn/releases
    if [ "$release" = latest ]; then base=$base/latest/download
    else base=$base/download/$release; fi
    download() {
        if have curl; then curl --fail --show-error --silent --location "$1" -o "$2"
        elif have wget; then wget -q -O "$2" "$1"
        else echo "Install curl or wget, then rerun the installer." >&2; return 1; fi
    }
    download "$base/$asset" "$SOURCE"
    download "$base/$asset.sha256" "$DOWNLOAD_DIR/checksum"
    expected=$(awk 'NR == 1 { print $1 }' "$DOWNLOAD_DIR/checksum")
    if have sha256sum; then actual=$(sha256sum "$SOURCE")
    elif have shasum; then actual=$(shasum -a 256 "$SOURCE")
    elif have sha256; then actual=$(sha256 -q "$SOURCE")
    elif have openssl; then actual=$(openssl dgst -sha256 -r "$SOURCE")
    else echo "SHA-256 verification requires sha256sum, shasum, sha256, or openssl." >&2; exit 1; fi
    actual=${actual%% *}
    if [ ${#expected} -ne 64 ] || [ "$expected" != "$actual" ]; then
        echo "Release checksum mismatch; installation stopped." >&2; exit 1
    fi
    chmod 755 "$SOURCE"
fi
"$SOURCE" --version

if [ "$WANT_HPC" = 1 ] && [ -z "$OCPROXY" ]; then
    say "Building ocproxy into ~/.local/bin (rootless VPN session for ssh)"
    src=${XDG_CACHE_HOME:-$HOME/.cache}/rwth-vpn-build/ocproxy
    rm -rf "$src" && mkdir -p "$(dirname "$src")"
    if git clone -q --depth 1 https://github.com/cernekee/ocproxy.git "$src" &&
        (cd "$src" && ./autogen.sh >/dev/null 2>&1 &&
            ./configure --prefix="$HOME/.local" --disable-vpnns >/dev/null &&
            make >/dev/null 2>&1) &&
        mkdir -p "$HOME/.local/bin" && cp "$src/ocproxy" "$HOME/.local/bin/ocproxy"; then
        echo "built $HOME/.local/bin/ocproxy"
    else
        warn "ocproxy build failed (cd $src && make); ssh via the VPN needs 'rwth-vpn up' meanwhile"
    fi
fi

if [ "$WANT_SPLIT" = 1 ]; then
    say "Installing vpn-slice (for rwth-vpn up --split; Linux and macOS only)"
    case "$OS" in
        Linux|Darwin)
            UV=""
            [ -x "$HOME/.local/bin/uv" ] && UV=$HOME/.local/bin/uv
            [ -z "$UV" ] && have uv && UV=$(command -v uv)
            if have vpn-slice || [ -x "$HOME/.local/bin/vpn-slice" ]; then
                echo "already installed"
            elif [ -n "$UV" ] && "$UV" tool --help >/dev/null 2>&1; then
                "$UV" tool install vpn-slice || warn "vpn-slice install failed; --split unavailable"
            elif have pipx; then
                pipx install vpn-slice || warn "vpn-slice install failed; --split unavailable"
            else
                echo "skipped: install uv (https://docs.astral.sh/uv/) or pipx, then rerun"
            fi
            ;;
        *) echo "skipped: vpn-slice does not support $OS";;
    esac
fi

say "Installing rwth-vpn into PATH"
# A user-writable directory: Homebrew's bin on macOS, else ~/.local/bin.
BIN=$HOME/.local/bin
if have brew && [ -w "$(brew --prefix)/bin" ]; then BIN=$(brew --prefix)/bin; fi
mkdir -p "$BIN"
# Replace atomically, keeping a working installation if copying fails.
staged=$(mktemp "$BIN/.rwth-vpn.XXXXXXXX")
if cp "$SOURCE" "$staged" && chmod 755 "$staged"; then
    mv -f "$staged" "$BIN/rwth-vpn"
else
    rm -f "$staged"; exit 1
fi
echo "installed $BIN/rwth-vpn"
case ":$PATH:" in
    *":$BIN:"*) ;;
    *) warn "$BIN is not on your PATH; add it in your shell profile";;
esac

CFG="${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/config"
if [ -n "$RWTH_USER" ] && [ ! -e "$CFG" ]; then
    mkdir -p "$(dirname "$CFG")"
    printf 'RWTH_USER=%s\n# RWTH_MODE=split\n' "$RWTH_USER" >"$CFG"
    echo "wrote $CFG"
fi

mkdir -p "$(dirname "$CFG")"
if [ "$WANT_HPC" = 1 ]; then
    echo hpc >"$(dirname "$CFG")/setup-profile"
    say "Setting up ssh for the RWTH HPC"
    "$BIN/rwth-vpn" ssh-setup
else
    echo vpn-only >"$(dirname "$CFG")/setup-profile"
fi

say "Checking the server"
"$BIN/rwth-vpn" check || true

if [ -t 0 ] && [ -t 1 ]; then
    printf '\nStore your logins and TOTP secrets now, so nothing ever prompts? [Y/n] '
    read -r ans || ans=n
    case "$ans" in [Nn]*) ;; *) "$BIN/rwth-vpn" secrets;; esac
fi

cat <<EOF

Done.
  rwth-vpn up           whole machine via RWTH (papers; asks for sudo)
  rwth-vpn up --split   only RWTH networks via the VPN
  rwth-vpn status | down | secrets | totp
EOF
if [ "$WANT_HPC" = 1 ]; then
    echo "  ssh claix             HPC login: no sudo, no prompts (VPN starts on demand)"
fi
