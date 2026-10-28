#!/bin/sh
# Copy the repo (working tree) into container $1, root-owned (read-only for tester).
set -eu
c=$1 repo=$2
arch=$(docker exec "$c" uname -m)
case "$arch" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo "unsupported lab architecture: $arch" >&2; exit 1;; esac
(cd "$repo" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -o dist/rwth-vpn-lab ./cmd/rwth-vpn)
docker exec "$c" rm -rf /opt/rwth-vpn
tar -C "$repo" --exclude=./.git --exclude=./lab -cf - . | docker exec -i "$c" sh -c \
    'mkdir -p /opt/rwth-vpn && tar -C /opt/rwth-vpn -xf - && chown -R root:root /opt/rwth-vpn &&
     chmod -R go-w /opt/rwth-vpn &&
     chmod 755 /opt/rwth-vpn /opt/rwth-vpn/dist /opt/rwth-vpn/dist/rwth-vpn-lab &&
     ln -sf /opt/rwth-vpn/dist/rwth-vpn-lab /usr/local/bin/rwth-vpn'
