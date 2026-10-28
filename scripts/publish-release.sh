#!/bin/sh
# Run from the repository root after committing the release changes.
set -eu
version=${1:-}
case "$version" in v[0-9]*) ;; *) echo 'usage: make publish VERSION=v0.1.0' >&2; exit 2;; esac
case "$version" in *[!A-Za-z0-9.-]*) echo 'invalid release version' >&2; exit 2;; esac
git check-ref-format "refs/tags/$version"
if ! git diff --quiet || ! git diff --cached --quiet ||
    [ -n "$(git ls-files --others --exclude-standard)" ]; then
    echo 'Commit all source changes before publishing; binaries must match the tag.' >&2
    exit 1
fi
if git show-ref --verify --quiet "refs/tags/$version"; then
    echo "Tag $version already exists; choose a new version." >&2; exit 1
fi
# Distinguish an absent tag (2) from a failed network/authentication request.
if git ls-remote --exit-code --tags origin "refs/tags/$version" >/dev/null; then
    echo "Remote tag $version already exists." >&2; exit 1
else
    result=$?
    [ "$result" = 2 ] || exit "$result"
fi
gh auth status
notes=${RELEASE_NOTES:-scripts/release-notes.md}
[ -r "$notes" ] || { echo "Missing release notes: $notes" >&2; exit 1; }
make check
go test -race ./...
make release VERSION="$version"
case "$(uname -s)" in
    Linux)
        if command -v dbus-run-session >/dev/null && command -v gnome-keyring-daemon >/dev/null; then
            case "$(uname -m)" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) arch=;; esac
            if [ -n "$arch" ]; then sh tests/secret-service.sh "dist/rwth-vpn_linux_$arch"; fi
        fi;;
esac
git tag -a "$version" -m "rwth-vpn $version"
git push --atomic origin HEAD "refs/tags/$version"
# Explicit asset paths exclude local development and Docker lab executables.
gh release create "$version" dist/rwth-vpn_* dist/install.sh dist/install.sh.sha256 \
    --verify-tag --title "rwth-vpn $version" --notes-file "$notes"
