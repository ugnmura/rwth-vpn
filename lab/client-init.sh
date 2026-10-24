#!/bin/sh
# Configure user `tester` in a client container: secrets, server, ssh-setup.
# usage: client-init.sh CONTAINER [USER]
set -eu
LAB=$(cd "$(dirname "$0")" && pwd)
. "$LAB/lab.env"
c=$1 u=${2:-tester}
home=$(docker exec "$c" sh -c "getent passwd $u | cut -d: -f6")
docker cp "$LAB/password.txt" "$c:/tmp/pw"
docker cp "$LAB/hpc-password.txt" "$c:/tmp/hpw"
docker exec "$c" chown "$u" /tmp/pw /tmp/hpw
docker exec -u "$u" -w "$home" -e TOTP_SECRET="$TOTP_SECRET" -e HPC_TOTP_SECRET="$HPC_TOTP_SECRET" \
    -e SSHD_IP="$SSHD_IP" "$c" bash -c '
secret_get() { rwth-vpn _secret-get "$1"; }; secret_set() { printf %s "$2" | rwth-vpn _secret-set "$1"; }
secret_set user tester
secret_set vpn_password "$(cat /tmp/pw)"; rm -f /tmp/pw
secret_set totp_secret "$TOTP_SECRET"
secret_set hpc_password "$(cat /tmp/hpw)"; rm -f /tmp/hpw
secret_set hpc_totp_secret "$HPC_TOTP_SECRET"
mkdir -p ~/.config/rwth-vpn && echo "RWTH_SERVER=rwtest-ocserv" > ~/.config/rwth-vpn/config
rwth-vpn ssh-setup >/dev/null
sed -i "s/HostName login23-1.hpc.itc.rwth-aachen.de/HostName $SSHD_IP/" ~/.ssh/rwth-vpn.conf'
