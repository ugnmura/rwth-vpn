package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationBothDirections(t *testing.T) {
	home := isolatedHome(t)
	mock := mockNative(t, home)
	body := `SELF="$3"; select_storage unencrypted; secret_set user tester; secret_set vpn_password 'password with spaces'; select_storage encrypted; [ ! -f "$SECRETS_FILE" ]; [ "$(secret_get vpn_password)" = 'password with spaces' ]; select_storage unencrypted; [ "$(secret_get vpn_password)" = 'password with spaces' ]; [ ! -f "$HOME/mock-keyring" ]`
	cmd := exec.Command("bash", "-c", "source \"$1\"; "+body, "test", shellLibrary(t, home), testBinary, mock)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("migration failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(home, "config/rwth-vpn/storage-mode"))
	if err != nil || string(data) != "unencrypted\n" {
		t.Fatalf("wrong final mode: %s %v", data, err)
	}
}

func TestMigrationFailuresPreserveSource(t *testing.T) {
	for _, fail := range []string{"MOCK_LOCKED", "MOCK_FAIL_NAME", "MOCK_READ_FAIL_NAME"} {
		t.Run(fail, func(t *testing.T) {
			home := isolatedHome(t)
			mock := mockNative(t, home)
			path := filepath.Join(home, "config/rwth-vpn/secrets")
			if err := fixtureStore(t, path).Set("vpn_password", "original"); err != nil {
				t.Fatal(err)
			}
			value := "vpn_password"
			if fail == "MOCK_LOCKED" {
				value = "1"
			}
			t.Setenv(fail, value)
			cmd := exec.Command("bash", "-c", `source "$1"; SELF="$2"; select_storage encrypted`, "test", shellLibrary(t, home), mock)
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("migration accepted failure: %s", out)
			}
			got, err := fixtureStore(t, path).Get("vpn_password")
			if err != nil || got != "original" {
				t.Fatalf("lost original credentials: %q %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(home, "config/rwth-vpn/storage-mode")); !os.IsNotExist(err) {
				t.Fatal("committed failed mode change")
			}
		})
	}
}

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
