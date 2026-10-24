#!/bin/sh
# Copy the repo (working tree) into container $1, root-owned (read-only for tester).
set -eu
c=$1 repo=$2
docker exec "$c" rm -rf /opt/rwth-vpn
tar -C "$repo" --exclude=./.git --exclude=./lab -cf - . | docker exec -i "$c" sh -c \
    'mkdir -p /opt/rwth-vpn && tar -C /opt/rwth-vpn -xf - && chown -R root:root /opt/rwth-vpn &&
     chmod -R go-w /opt/rwth-vpn && ln -sf /opt/rwth-vpn/rwth-vpn /usr/local/bin/rwth-vpn'
