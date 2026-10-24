#!/bin/sh
# Install rwth-vpn for the current user. Never uses sudo: everything goes into
# your home directory, and missing system packages are listed with the one
# command to install them. Only `rwth-vpn up` (whole-machine tunnel) needs
# root, when you run it.
# POSIX sh on purpose: Void, Alpine and the BSDs don't ship bash by default.
#
#   ./install.sh [--no-split] [--user ab123456]

set -eu

REPO=$(cd "$(dirname "$0")" && pwd)
WANT_SPLIT=1
RWTH_USER=""

while [ $# -gt 0 ]; do
    case "$1" in
        --no-split) WANT_SPLIT=0;;
        --user) RWTH_USER=${2:?--user needs a value}; shift;;
        *) echo "unknown option: $1" >&2; exit 2;;
    esac
    shift
done

say() { printf '\n==> %s\n' "$*"; }
warn() { echo "warning: $*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

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
        python3:pacman|python3:brew) echo python;;
        python3:emerge) echo dev-lang/python;;
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
have python3 || add python3
have ssh || add openssh
have bash || add bash
have_vpnc_script || NEED_UP="$NEED_UP $(pkg vpnc-script)"
[ "$OS" != Linux ] || have ip || NEED_UP="$NEED_UP $(pkg ip)"

OCPROXY=""
if have ocproxy; then OCPROXY=$(command -v ocproxy)
elif [ -x "$HOME/.local/bin/ocproxy" ]; then OCPROXY=$HOME/.local/bin/ocproxy
elif [ -n "$(pkg ocproxy)" ]; then add ocproxy
elif ! { have cc && have make && have autoconf && have automake && have_libevent; }; then
    NEED="$NEED $(pkg build)"
fi
have git || [ -n "$OCPROXY" ] || [ -n "$(pkg ocproxy)" ] || add git

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

if [ -z "$OCPROXY" ]; then
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
# A copy, not a link: the script is self-contained, so the clone can go.
rm -f "$BIN/rwth-vpn"
cp "$REPO/rwth-vpn" "$BIN/rwth-vpn" && chmod 755 "$BIN/rwth-vpn"
echo "installed $BIN/rwth-vpn"
case ":$PATH:" in
    *":$BIN:"*) ;;
    *) warn "$BIN is not on your PATH; add it in your shell profile (ssh works regardless)";;
esac

CFG="${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/config"
if [ -n "$RWTH_USER" ] && [ ! -e "$CFG" ]; then
    mkdir -p "$(dirname "$CFG")"
    printf 'RWTH_USER=%s\n# RWTH_MODE=split\n' "$RWTH_USER" >"$CFG"
    echo "wrote $CFG"
fi

say "Setting up ssh for the RWTH HPC"
"$BIN/rwth-vpn" ssh-setup

say "Checking the server"
"$BIN/rwth-vpn" check || true

if [ -t 0 ] && [ -t 1 ]; then
    printf '\nStore your logins and TOTP secrets now, so nothing ever prompts? [Y/n] '
    read -r ans || ans=n
    case "$ans" in [Nn]*) ;; *) "$BIN/rwth-vpn" secrets;; esac
fi

cat <<EOF

Done.
  ssh claix             HPC login: no sudo, no prompts (VPN starts on demand)
  rwth-vpn up           whole machine via RWTH (papers; asks for sudo)
  rwth-vpn up --split   only RWTH networks via the VPN
  rwth-vpn status | down | secrets | totp
EOF
