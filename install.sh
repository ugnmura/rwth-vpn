#!/bin/sh
# Install rwth-vpn: openconnect + vpnc-script, bash, iproute2 (Linux), optional
# vpn-slice, and the `rwth-vpn` command on PATH.
# POSIX sh on purpose: Void, Alpine and the BSDs don't ship bash by default.
#
#   ./install.sh [--no-split] [--user ab123456]
#
# Linux: Arch/Manjaro, Debian/Ubuntu, Fedora/RHEL (EPEL), openSUSE, Void,
#        Alpine, Gentoo, Solus, NixOS (prints instructions).
# macOS: Homebrew or MacPorts.  BSD: FreeBSD, OpenBSD, NetBSD (full mode only).

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

ROOT=""
if [ "$(id -u)" -ne 0 ]; then
    if have sudo; then ROOT=sudo
    elif have doas; then ROOT=doas
    else echo "need sudo or doas (or run as root)" >&2; exit 1
    fi
fi

OS=$(uname -s)

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

# Install packages $1 (required) and, if vpnc-script is still missing, try the
# candidate names in $2 one by one (they differ per distro and release).
pkg_install() {
    req=$1 extra=$2
    case "$PM" in
        pacman)
            # Only sync when there's no database at all (fresh container/install);
            # a bare -Sy on a live system would mean a partial upgrade.
            sy=-S
            ls /var/lib/pacman/sync/*.db >/dev/null 2>&1 || sy=-Sy
            $ROOT pacman $sy --needed --noconfirm $req;;
        apt) $ROOT apt-get update && $ROOT apt-get install -y $req;;
        dnf) $ROOT dnf install -y $req;;
        yum) $ROOT yum install -y $req;;
        zypper) $ROOT zypper --non-interactive install $req;;
        xbps) $ROOT xbps-install -Sy $req;;
        apk) $ROOT apk add $req;;
        emerge) $ROOT emerge --noreplace $req;;
        eopkg) $ROOT eopkg install -y $req;;
        brew) brew install $req;;
        port) $ROOT port install $req;;
        pkg) $ROOT pkg install -y $req;;
        pkg_add) $ROOT pkg_add $req;;
        pkgin) $ROOT pkgin -y install $req;;
    esac
    have_vpnc_script && return 0
    for e in $extra; do
        case "$PM" in
            pacman) $ROOT pacman -S --needed --noconfirm "$e";;
            apt) $ROOT apt-get install -y "$e";;
            dnf|yum) $ROOT "$PM" install -y "$e";;
            zypper) $ROOT zypper --non-interactive install "$e";;
            xbps) $ROOT xbps-install -y "$e";;
            apk) $ROOT apk add "$e";;
            *) false;;
        esac 2>/dev/null && have_vpnc_script && return 0
    done
    have_vpnc_script || warn "vpnc-script still missing; install your system's vpnc-scripts package"
}

say "Installing openconnect"
if [ "$OS" = Darwin ]; then
    if have brew; then PM=brew
    elif have port; then PM=port
    else echo "install Homebrew (https://brew.sh) or MacPorts first" >&2; exit 1
    fi
    if have openconnect && have_vpnc_script; then
        echo "already installed: $(openconnect --version | head -n 1)"
    else
        pkg_install openconnect ""
    fi
elif [ -e /etc/NIXOS ] || { have nix-env && ! have apt-get && ! have pacman && ! have dnf; }; then
    if have openconnect; then
        echo "already installed: $(openconnect --version | head -n 1)"
    else
        echo "NixOS: add 'openconnect' to environment.systemPackages (or: nix profile install nixpkgs#openconnect), then rerun." >&2
        exit 1
    fi
else
    PM=""
    for c in pacman apt-get dnf yum zypper xbps-install apk emerge eopkg pkgin pkg_add pkg; do
        if have "$c"; then PM=${c%-get}; PM=${PM%-install}; break; fi
    done
    [ -n "$PM" ] || { echo "unknown package manager: install openconnect, vpnc-script and bash yourself" >&2; exit 1; }
    case "$OS" in
        Linux)
            case "$PM" in
                emerge) pkgs="net-vpn/openconnect sys-apps/iproute2 app-shells/bash";;
                *) pkgs="openconnect iproute2 bash";;
            esac
            [ "$PM" = dnf ] || [ "$PM" = yum ] && pkgs="openconnect iproute bash"
            ;;
        *) pkgs="openconnect bash";;
    esac
    if have openconnect && have_vpnc_script && have bash && { [ "$OS" != Linux ] || have ip; }; then
        echo "already installed: $(openconnect --version | head -n 1)"
    else
        pkg_install "$pkgs" "vpnc-scripts vpnc-script vpnc"
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

say "Linking rwth-vpn into PATH"
chmod +x "$REPO/rwth-vpn" "$REPO/lib/hook.sh"
if [ -d /usr/local/bin ]; then
    $ROOT ln -sf "$REPO/rwth-vpn" /usr/local/bin/rwth-vpn
    echo "/usr/local/bin/rwth-vpn -> $REPO/rwth-vpn"
else
    mkdir -p "$HOME/.local/bin"
    ln -sf "$REPO/rwth-vpn" "$HOME/.local/bin/rwth-vpn"
    echo "$HOME/.local/bin/rwth-vpn -> $REPO/rwth-vpn"
fi

CFG="${XDG_CONFIG_HOME:-$HOME/.config}/rwth-vpn/config"
if [ ! -e "$CFG" ]; then
    if [ -z "$RWTH_USER" ] && [ -t 0 ]; then
        printf '\nRWTH username (e.g. ab123456; empty = ask on every login): '
        read -r RWTH_USER || true
    fi
    if [ -n "$RWTH_USER" ]; then
        mkdir -p "$(dirname "$CFG")"
        printf 'RWTH_USER=%s\n# RWTH_MODE=split\n' "$RWTH_USER" >"$CFG"
        echo "wrote $CFG"
    fi
fi

say "Checking the server"
"$REPO/rwth-vpn" check || true

cat <<EOF

Done.
  rwth-vpn up           connect (all traffic via RWTH; Tailscale stays direct)
  rwth-vpn up --split   only RWTH networks via the VPN
  rwth-vpn status
  rwth-vpn down
EOF
