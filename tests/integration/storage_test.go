package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEncryptedUnavailableDoesNotCreatePlaintext(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux D-Bus failure test")
	}
	home := isolatedHome(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(home, "no-such-bus"))
	writeTestFile(t, filepath.Join(home, "config/rwth-vpn/storage-mode"), "encrypted\n", 0600)
	out, err := runBinary(t, "do-not-log-this-password", "_secret-set", "vpn_password")
	if err == nil || !strings.Contains(out, "secrets --unlock") {
		t.Fatalf("missing keyring failure guidance: %v %s", err, out)
	}
	if strings.Contains(out, "do-not-log-this-password") {
		t.Fatal("credential leaked in diagnostic")
	}
	if _, err := os.Stat(filepath.Join(home, "config/rwth-vpn/secrets")); !os.IsNotExist(err) {
		t.Fatal("encrypted mode created plaintext fallback")
	}
}

func TestUnlockRequiresTerminal(t *testing.T) {
	isolatedHome(t)
	out, err := runBinary(t, "", "secrets", "--mode", "encrypted", "--unlock")
	if err == nil || !strings.Contains(out, "interactive terminal") {
		t.Fatalf("noninteractive unlock: %v %s", err, out)
	}
}

func TestInvalidStorageModeDoesNotFallBack(t *testing.T) {
	for _, mode := range []string{"", "invalid\n"} {
		t.Run(fmt.Sprintf("%q", mode), func(t *testing.T) {
			home := isolatedHome(t)
			writeTestFile(t, filepath.Join(home, "config/rwth-vpn/storage-mode"), mode, 0600)
			out, err := runBinary(t, "password", "_secret-set", "vpn_password")
			if err == nil {
				t.Fatalf("accepted corrupt storage mode: %s", out)
			}
			if _, err := os.Stat(filepath.Join(home, "config/rwth-vpn/secrets")); !os.IsNotExist(err) {
				t.Fatal("corrupt mode caused plaintext fallback")
			}
		})
	}
}
