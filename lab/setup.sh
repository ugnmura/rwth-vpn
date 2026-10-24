#!/bin/sh
# Build and start the rwth-vpn test lab. Rerunnable; teardown.sh removes it.
set -eu
LAB=$(cd "$(dirname "$0")" && pwd)
REPO=${REPO:-$LAB/..}
. "$LAB/lab.env"
cd "$LAB"

mkdir -p certs
if [ ! -s certs/ca.crt ]; then
    openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj "/CN=rwtest CA" \
        -keyout certs/ca.key -out certs/ca.crt 2>/dev/null
    openssl req -newkey rsa:2048 -nodes -subj "/CN=rwtest-ocserv" \
        -keyout certs/server.key -out certs/server.csr 2>/dev/null
    printf 'subjectAltName=DNS:rwtest-ocserv\nextendedKeyUsage=serverAuth\n' > certs/ext.cnf
    openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial \
        -days 3650 -extfile certs/ext.cnf -out certs/server.crt 2>/dev/null
    chmod 644 certs/server.key
fi
openssl passwd -6 -stdin < password.txt > ocserv.hash
cp certs/ca.crt client/ca.crt

docker build -q -t rwtest-ocserv ocserv
docker build -q -t rwtest-sshd sshd
docker build -q -t rwtest-client client

docker network inspect rwtest-inside >/dev/null 2>&1 ||
    docker network create --internal --subnet "$INSIDE_NET" rwtest-inside
docker network inspect rwtest-outside >/dev/null 2>&1 ||
    docker network create --subnet "$OUTSIDE_NET" rwtest-outside

docker rm -f rwtest-ocserv rwtest-sshd >/dev/null 2>&1 || true
docker run -d --name rwtest-ocserv --hostname rwtest-ocserv --network rwtest-outside \
    --cap-add NET_ADMIN --device /dev/net/tun --sysctl net.ipv4.ip_forward=1 \
    -v "$LAB:/lab:ro" rwtest-ocserv >/dev/null
docker network connect --ip "$OCSERV_INSIDE_IP" rwtest-inside rwtest-ocserv
docker run -d --name rwtest-sshd --hostname claix-test-sshd --network rwtest-inside --ip "$SSHD_IP" \
    -v "$LAB:/lab:ro" rwtest-sshd >/dev/null
# sshd's replies to VPN clients go back via ocserv.
docker run --rm --network container:rwtest-sshd --cap-add NET_ADMIN rwtest-client \
    ip route replace 192.168.99.0/24 via "$OCSERV_INSIDE_IP"

# Clients: user (rootless) and root (system tunnel), both outside only.
for c in rwtest-client rwtest-root; do
    docker rm -f "$c" >/dev/null 2>&1 || true
    extra=""
    [ "$c" = rwtest-root ] && extra="--cap-add NET_ADMIN --device /dev/net/tun"
    # shellcheck disable=SC2086
    docker run -d --init --name "$c" --hostname "$c" --network rwtest-outside $extra rwtest-client >/dev/null
    "$LAB/install-repo.sh" "$c" "$REPO"
    "$LAB/client-init.sh" "$c" "$( [ "$c" = rwtest-root ] && echo root || echo tester)"
done
echo "lab up"
