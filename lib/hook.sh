#!/bin/sh
# openconnect --script hook for rwth-vpn. Runs as root.
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
    cat >"$RWTH_VPN_STATE" <<EOF
MODE='$RWTH_VPN_MODE'
TUNDEV='$TUNDEV'
IP4='$INTERNAL_IP4_ADDRESS'
IP6='${INTERNAL_IP6_ADDRESS:-}'
SINCE='$(date '+%Y-%m-%d %H:%M')'
EOF
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

case "$RWTH_VPN_MODE" in
split)
    case "$reason" in
        connect) write_state;;
        disconnect) rm -f "$RWTH_VPN_STATE";;
    esac
    # shellcheck disable=SC2086
    exec "$RWTH_VPN_SLICE" -S -I $RWTH_VPN_SPLIT_ROUTES
    ;;
esac

# full mode
if [ "$(uname -s)" != Linux ]; then
    case "$reason" in
        connect) "$RWTH_VPN_VPNC_SCRIPT" && write_state; exit;;
        disconnect) rm -f "$RWTH_VPN_STATE";;
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
    fi
    write_state
    ;;
disconnect)
    "$RWTH_VPN_VPNC_SCRIPT"
    linux_full_cleanup
    rm -f "$RWTH_VPN_STATE"
    ;;
*)
    exec "$RWTH_VPN_VPNC_SCRIPT"
    ;;
esac
