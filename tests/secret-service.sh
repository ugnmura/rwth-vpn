#!/bin/sh
# Real Secret Service integration, isolated from your session and keyrings.
# Requires dbus-run-session, dbus-send and gnome-keyring-daemon, only for this test.
set -eu
for dependency in dbus-run-session dbus-send gnome-keyring-daemon; do
    command -v "$dependency" >/dev/null 2>&1 || {
        echo "Missing test dependency: $dependency" >&2; exit 1
    }
done
if [ "${2:-}" != inner ]; then
    binary=$(cd "$(dirname "${1:-dist/rwth-vpn}")" && pwd)/$(basename "${1:-dist/rwth-vpn}")
    test_home=$(mktemp -d "${TMPDIR:-/tmp}/rwth-vpn-keyring.XXXXXXXX")
    trap 'rm -rf "$test_home"' 0
    mkdir -p "$test_home/run" "$test_home/config" "$test_home/data"
    chmod 700 "$test_home/run"
    (
        unset GNOME_KEYRING_CONTROL GNOME_KEYRING_PID DBUS_SESSION_BUS_ADDRESS DBUS_SESSION_BUS_PID
        HOME=$test_home XDG_RUNTIME_DIR=$test_home/run XDG_CONFIG_HOME=$test_home/config \
            XDG_DATA_HOME=$test_home/data DISPLAY= WAYLAND_DISPLAY= \
            dbus-run-session -- sh "$0" "$binary" inner
    )
    exit
fi
binary=$1
printf '%s' 'rwth-vpn-test-keyring-password' |
    gnome-keyring-daemon --foreground --unlock --components=secrets >"$HOME/daemon.log" 2>&1 &
daemon=$!
trap 'kill "$daemon" 2>/dev/null || true; wait "$daemon" 2>/dev/null || true' 0
# Query the bus itself without activating Secret Service. Calling the client
# too early can auto-start a competing daemon while the login keyring is being
# created, corrupting this test's temporary keyring.
n=0
until dbus-send --session --print-reply --dest=org.freedesktop.DBus \
    /org/freedesktop/DBus org.freedesktop.DBus.NameHasOwner \
    string:org.freedesktop.secrets | grep -q 'boolean true'; do
    n=$((n + 1))
    if ! kill -0 "$daemon" 2>/dev/null || [ "$n" -ge 50 ]; then
        cat "$HOME/daemon.log" >&2
        echo 'FAIL: test keyring did not acquire its D-Bus name' >&2; exit 1
    fi
    sleep 0.1
done
n=0
while ! printf '%s' 'test-vpn-password' | "$binary" __native secret-set secret-service unused vpn_password 2>"$HOME/error"; do
    n=$((n + 1))
    if [ "$n" -ge 10 ]; then cat "$HOME/error" "$HOME/daemon.log" >&2; exit 1; fi
    sleep 0.2
done
[ "$("$binary" __native secret-get secret-service unused vpn_password)" = test-vpn-password ]
"$binary" __native secret-purge secret-service
if "$binary" __native secret-get secret-service unused vpn_password; then
    echo 'FAIL: credential survived purge' >&2; exit 1
else
    [ "$?" = 1 ]
fi
echo 'PASS: real isolated Secret Service store, lookup, and purge'
