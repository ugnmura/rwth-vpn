#!/bin/sh
set -eu
version=${1:-dev}
case "$version" in ''|*[!A-Za-z0-9._-]*) echo "invalid release version" >&2; exit 2;; esac
mkdir -p dist
sed "s/^DEFAULT_VERSION=latest$/DEFAULT_VERSION=$version/" install.sh >dist/install.sh
if command -v sha256sum >/dev/null 2>&1; then
    (cd dist && sha256sum install.sh >install.sh.sha256)
else
    (cd dist && shasum -a 256 install.sh >install.sh.sha256)
fi
for os in linux darwin freebsd openbsd netbsd; do
    for arch in amd64 arm64; do
        asset=rwth-vpn_${os}_${arch}
        CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
            -ldflags "-s -w -X main.version=$version" -o "dist/$asset" ./cmd/rwth-vpn
        (cd dist && if command -v sha256sum >/dev/null 2>&1; then
            sha256sum "$asset" >"$asset.sha256"
        else
            shasum -a 256 "$asset" >"$asset.sha256"
        fi)
    done
done
