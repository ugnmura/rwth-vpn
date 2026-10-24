#!/bin/sh
set -e
. /lab/lab.env
mkdir -p /etc/ocserv/groups
# Full Tunnel: default route (like the ASA); Split Tunnel: only the inside net.
echo "route = default" > /etc/ocserv/groups/full
echo "route = $INSIDE_NET" > /etc/ocserv/groups/split
echo "$VPN_USER:full,split:$(cat /lab/ocserv.hash)" > /etc/ocserv/passwd
# liboath users file (VPN token)
OTP_FILE=/etc/ocserv/users.otp
[ -s "$OTP_FILE" ] || echo "HOTP/T30/6 $VPN_USER - $TOTP_HEX" > "$OTP_FILE"
iptables -t nat -A POSTROUTING -s 192.168.99.0/24 -j MASQUERADE
exec ocserv -f -d 3 -c /etc/ocserv/ocserv.conf
