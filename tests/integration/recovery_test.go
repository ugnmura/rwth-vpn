package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepareRecovery(t *testing.T, fail bool) string {
	t.Helper()
	home := isolatedHome(t)
	hook := `#!/bin/sh
set -eu
[ "$1" = _hook ]
[ "$reason" = disconnect ]
[ "$VPNPID" = 12345 ]
printf '%s' "$INTERNAL_IP4_DNS" > "$HOME/restored-dns"
`
	if fail {
		hook += "exit 7\n"
	}
	writeTestFile(t, filepath.Join(home, "hook"), hook, 0700)
	return home
}

const recoveryFixture = `
RUN_DIR="$HOME/recovery"
PID_FILE="$RUN_DIR/openconnect.pid"
STATE_FILE="$RUN_DIR/state"
RECOVERY_FILE="$RUN_DIR/disconnect.env"
SELF="$HOME/hook"
LOG_FILE="$HOME/recovery.log"
mkdir -p "$RUN_DIR"
is_root() { return 0; }
export VPNPID=12345 TUNDEV=tun-rwth INTERNAL_IP4_DNS=192.0.2.53
export CISCO_DEF_DOMAIN='example.test; touch should-not-run'
export COOKIE=do-not-save PASSWORD=do-not-save
save_recovery
printf '12345\n' >"$PID_FILE"
printf 'state\n' >"$STATE_FILE"
`

func TestDownRecoversAfterProcessExit(t *testing.T) {
	home := prepareRecovery(t, false)
	out, err := runLibrary(t, home, recoveryFixture+`unset VPNPID INTERNAL_IP4_DNS; cmd_down; cmd_down`)
	if err != nil || !strings.Contains(out, "restored routing and DNS") {
		t.Fatalf("recovery: %v %s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(home, "restored-dns"))
	if err != nil || string(data) != "192.0.2.53" {
		t.Fatalf("hook environment not restored: %s %v", data, err)
	}
	for _, name := range []string{"openconnect.pid", "state", "disconnect.env"} {
		if _, err := os.Stat(filepath.Join(home, "recovery", name)); !os.IsNotExist(err) {
			t.Errorf("stale file kept: %s", name)
		}
	}
}

func TestRecoveryFailureKeepsRetryData(t *testing.T) {
	home := prepareRecovery(t, true)
	out, err := runLibrary(t, home, recoveryFixture+`cmd_down`)
	if err == nil || !strings.Contains(out, "recovery data kept") {
		t.Fatalf("failed cleanup not reported: %v %s", err, out)
	}
	path := filepath.Join(home, "recovery/disconnect.env")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("recovery file removed after failed hook")
	}
	if strings.Contains(string(data), "do-not-save") {
		t.Fatal("saved authentication values in recovery data")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("insecure recovery permissions: %v", err)
	}
}

func TestTLSOnlyIsDefault(t *testing.T) {
	home := isolatedHome(t)
	out, err := runLibrary(t, home, `unset RWTH_DTLS; load_config --no-secrets; test "$RWTH_DTLS" = 0`)
	if err != nil {
		t.Fatalf("TLS-only default: %v %s", err, out)
	}
}

func TestZombieIsNotAnActiveVPN(t *testing.T) {
	home := isolatedHome(t)
	out, err := runLibrary(t, home, `ps() { printf ' Zs\n'; }; if process_running 12345; then exit 1; fi; ps() { printf ' Ssl\n'; }; process_running 12345`)
	if err != nil {
		t.Fatalf("process-state handling: %v %s", err, out)
	}
}

func TestConnectionMetadataReadableWithPrivateUmask(t *testing.T) {
	home := isolatedHome(t)
	out, err := runLibrary(t, home, `RUN_DIR="$HOME/state-dir"; mkdir -p "$RUN_DIR"; RWTH_VPN_STATE="$RUN_DIR/state"; RWTH_VPN_MODE=full; TUNDEV=tun-rwth; INTERNAL_IP4_ADDRESS=192.0.2.1; umask 077; write_state`)
	if err != nil {
		t.Fatalf("state writing: %v %s", err, out)
	}
	info, err := os.Stat(filepath.Join(home, "state-dir/state"))
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("connection state is unreadable: %v", err)
	}
}
