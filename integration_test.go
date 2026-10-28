package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "rwth-vpn-tests-")
	if err != nil {
		panic(err)
	}
	testBinary = filepath.Join(dir, "rwth-vpn")
	build := exec.Command("go", "build", "-o", testBinary, ".")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "run"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	return home
}
func runBinary(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(testBinary, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func shellLibrary(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, "library.sh")
	writeTestFile(t, path, strings.TrimSuffix(script, "main \"$@\"\n"), 0600)
	return path
}
func runLibrary(t *testing.T, home, body string) (string, error) {
	t.Helper()
	lib := shellLibrary(t, home)
	cmd := exec.Command("bash", "-c", "source \"$1\"; SELF=\"$2\"; is_running() { return 1; }; "+body, "test", lib, testBinary)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func mockNative(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, "mock-native")
	writeTestFile(t, path, `#!/bin/sh
set -eu
op=$2
if [ "$op" = secret-purge ]; then rm -f "$HOME/mock-keyring"; exit 0; fi
backend=$3
file=$4
name=$5
if [ "$backend" != file ]; then
    if [ "${MOCK_LOCKED:-0}" = 1 ]; then echo 'keyring locked' >&2; exit 3; fi
    if [ "$op" = secret-set ] && [ "$name" = "${MOCK_FAIL_NAME:-}" ]; then exit 3; fi
    if [ "$op" = secret-get ] && [ "$name" = "${MOCK_READ_FAIL_NAME:-}" ]; then exit 3; fi
    file=$HOME/mock-keyring
fi
exec "$REAL_BINARY" __native "$op" file "$file" "$name"
`, 0700)
	t.Setenv("REAL_BINARY", testBinary)
	return path
}
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
			if err := (fileStore{path}).Set("vpn_password", "original"); err != nil {
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
			got, err := (fileStore{path}).Get("vpn_password")
			if err != nil || got != "original" {
				t.Fatalf("lost original credentials: %q %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(home, "config/rwth-vpn/storage-mode")); !os.IsNotExist(err) {
				t.Fatal("committed failed mode change")
			}
		})
	}
}
func TestUninstallPreservesOrPurges(t *testing.T) {
	for _, purge := range []bool{false, true} {
		t.Run(fmt.Sprint(purge), func(t *testing.T) {
			home := isolatedHome(t)
			app := filepath.Join(home, "bin/rwth-vpn")
			data, err := os.ReadFile(testBinary)
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, app, string(data), 0700)
			conf := filepath.Join(home, ".ssh/config")
			keep := "Host personal\n    HostName example.org\n"
			writeTestFile(t, conf, "Include "+filepath.Join(home, ".ssh/rwth-vpn.conf")+"\n"+keep, 0600)
			writeTestFile(t, filepath.Join(home, ".ssh/rwth-vpn.conf"), "generated", 0600)
			dir := filepath.Join(home, "config/rwth-vpn")
			writeTestFile(t, filepath.Join(dir, "config"), "invalid shell (\n", 0600)
			writeTestFile(t, filepath.Join(dir, "storage-mode"), "unencrypted\n", 0600)
			writeTestFile(t, filepath.Join(dir, "secrets"), "user=dGVzdA==\n", 0600)
			args := []string{"uninstall"}
			if purge {
				args = append(args, "--purge")
			}
			out, err := exec.Command(app, args...).CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if _, err := os.Stat(app); !os.IsNotExist(err) {
				t.Fatal("binary kept")
			}
			got, _ := os.ReadFile(conf)
			if string(got) != keep {
				t.Fatal("unrelated SSH config changed")
			}
			_, err = os.Stat(filepath.Join(dir, "secrets"))
			if os.IsNotExist(err) != purge {
				t.Fatalf("purge=%v credentials existence wrong: %v", purge, err)
			}
		})
	}
}
func TestVPNOnlyCredentialSetup(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("PTY test requires script")
	}
	home := isolatedHome(t)
	writeTestFile(t, filepath.Join(home, "config/rwth-vpn/storage-mode"), "unencrypted\n", 0600)
	// Call the compiled CLI in a PTY so this exercises the real interactive path.
	t.Setenv("TEST_APP", testBinary)
	var cmd *exec.Cmd
	if runtime.GOOS == "linux" {
		cmd = exec.Command("script", "-q", "-e", "-c", `"$TEST_APP" secrets --no-hpc`, "/dev/null")
	} else {
		cmd = exec.Command("script", "-q", "/dev/null", "sh", "-c", `"$TEST_APP" secrets --no-hpc`)
	}
	cmd.Stdin = strings.NewReader(strings.Repeat("\n", 8))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("VPN-only setup: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "HPC password") {
		t.Fatalf("HPC prompt in VPN-only setup: %s", out)
	}
	if !strings.Contains(string(out), "VPN secrets incomplete") {
		t.Fatalf("setup did not finish: %s", out)
	}
	profile, err := os.ReadFile(filepath.Join(home, "config/rwth-vpn/setup-profile"))
	if err != nil || string(profile) != "vpn-only\n" {
		t.Fatalf("VPN-only profile was not saved: %s %v", profile, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh/rwth-vpn.conf")); !os.IsNotExist(err) {
		t.Fatal("VPN-only setup wrote SSH configuration")
	}
}
func TestCompiledTOTPCLI(t *testing.T) {
	isolatedHome(t)
	out, err := runBinary(t, "JBSWY3DPEHPK3PXP", "_totp")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if len(strings.TrimSpace(out)) != 6 {
		t.Fatalf("invalid code %q", out)
	}
}

func TestStandaloneInstaller(t *testing.T) {
	for _, scenario := range []string{"success", "wget", "pinned", "released-installer", "download-failure", "checksum-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			home := isolatedHome(t)
			t.Setenv("TMPDIR", home)
			t.Setenv("RWTH_VPN_VERSION", "")
			bin := filepath.Join(home, "tools")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			// Deliberately omit Python, Go, secret-tool, ssh, ocproxy and package managers.
			for _, tool := range []string{"dirname", "uname", "sed", "head", "awk", "mktemp", "mkdir", "chmod", "cp", "mv", "rm", "bash", "cat"} {
				path, err := exec.LookPath(tool)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
					t.Fatal(err)
				}
			}
			hashTool := "sha256sum"
			hashPath, err := exec.LookPath(hashTool)
			if err != nil {
				hashTool = "shasum"
				hashPath, err = exec.LookPath(hashTool)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(hashPath, filepath.Join(bin, hashTool)); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(bin, "vpnc-script"), "#!/bin/sh\nexit 0\n", 0700)
			writeTestFile(t, filepath.Join(bin, "ip"), "#!/bin/sh\nexit 0\n", 0700)
			writeTestFile(t, filepath.Join(bin, "openconnect"), "#!/bin/sh\ncase \"$1\" in --help) echo '    default: \""+filepath.Join(bin, "vpnc-script")+"\"';; *) echo 'OpenConnect version v9.20';; esac\n", 0700)
			payload := filepath.Join(home, "payload")
			writeTestFile(t, payload, "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'rwth-vpn test'; else echo \"$1\" >>\"$HOME/actions\"; fi\n", 0700)
			downloadTool := "curl"
			if scenario == "wget" {
				downloadTool = "wget"
			}
			writeTestFile(t, filepath.Join(bin, downloadTool), `#!/bin/sh
set -eu
url= dest=
while [ "$#" -gt 0 ]; do
 case "$1" in -o|-O) shift; dest=$1;; https://*) url=$1;; esac
 shift
done
printf '%s\n' "$url" >>"$HOME/download-urls"
[ "$SCENARIO" != download-failure ] || exit 22
case "$url" in
 *.sha256)
  if [ "$SCENARIO" = checksum-mismatch ]; then echo '0000000000000000000000000000000000000000000000000000000000000000 file' >"$dest"
  elif command -v sha256sum >/dev/null; then sha256sum "$PAYLOAD" >"$dest"
  else shasum -a 256 "$PAYLOAD" >"$dest"; fi;;
 *) cp "$PAYLOAD" "$dest";;
esac
`, 0700)
			installer, err := os.ReadFile("install.sh")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, "standalone/install.sh")
			if scenario == "released-installer" {
				installer = []byte(strings.Replace(string(installer), "DEFAULT_VERSION=latest", "DEFAULT_VERSION=v0.1.0", 1))
			}
			writeTestFile(t, path, string(installer), 0600)
			installed := filepath.Join(home, ".local/bin/rwth-vpn")
			writeTestFile(t, installed, "existing installation", 0700)
			cmd := exec.Command("/bin/sh", path, "--no-hpc")
			cmd.Env = append(os.Environ(), "PATH="+bin, "SCENARIO="+scenario, "PAYLOAD="+payload)
			if scenario == "pinned" {
				cmd.Env = append(cmd.Env, "RWTH_VPN_VERSION=v1.2.3")
			}
			out, err := cmd.CombinedOutput()
			leftovers, _ := filepath.Glob(filepath.Join(home, "rwth-vpn-install.*"))
			if len(leftovers) > 0 {
				t.Fatalf("download files not cleaned: %v", leftovers)
			}
			got, readErr := os.ReadFile(installed)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if scenario == "success" || scenario == "wget" || scenario == "pinned" || scenario == "released-installer" {
				if err != nil {
					t.Fatalf("install: %v\n%s", err, out)
				}
				urls, _ := os.ReadFile(filepath.Join(home, "download-urls"))
				asset := "rwth-vpn_" + runtime.GOOS + "_" + runtime.GOARCH
				if !strings.Contains(string(urls), asset) {
					t.Fatalf("wrong platform asset: %s", urls)
				}
				if scenario == "pinned" && !strings.Contains(string(urls), "/download/v1.2.3/") {
					t.Fatalf("version pin ignored: %s", urls)
				}
				if scenario == "released-installer" && !strings.Contains(string(urls), "/download/v0.1.0/") {
					t.Fatalf("release installer did not pin its version: %s", urls)
				}
				if string(got) == "existing installation" {
					t.Fatal("binary not replaced")
				}
				actions, _ := os.ReadFile(filepath.Join(home, "actions"))
				if string(actions) != "check\n" {
					t.Fatalf("unexpected optional setup: %s", actions)
				}
				if _, err := os.Stat(filepath.Join(home, ".ssh")); !os.IsNotExist(err) {
					t.Fatal("VPN-only install created SSH configuration")
				}
			} else {
				if err == nil {
					t.Fatalf("installer accepted %s: %s", scenario, out)
				}
				if string(got) != "existing installation" {
					t.Fatal("failed install replaced working binary")
				}
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
func TestUninstallRefusesActiveTunnel(t *testing.T) {
	home := isolatedHome(t)
	app := filepath.Join(home, "fake-app")
	writeTestFile(t, app, "keep me", 0700)
	out, err := runLibrary(t, home, `SELF="$HOME/fake-app"; is_running() { return 0; }; cmd_uninstall`)
	if err == nil || !strings.Contains(out, "rwth-vpn down") {
		t.Fatalf("did not refuse active tunnel: %v %s", err, out)
	}
	if data, err := os.ReadFile(app); err != nil || string(data) != "keep me" {
		t.Fatal("removed active tunnel executable")
	}
}
func TestUnlockRequiresTerminal(t *testing.T) {
	isolatedHome(t)
	out, err := runBinary(t, "", "secrets", "--mode", "encrypted", "--unlock")
	if err == nil || !strings.Contains(out, "interactive terminal") {
		t.Fatalf("noninteractive unlock: %v %s", err, out)
	}
}

func TestHelpDoesNotAccessKeyring(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(home, "no-such-bus"))
	writeTestFile(t, filepath.Join(home, "config/rwth-vpn/storage-mode"), "encrypted\n", 0600)
	out, err := runBinary(t, "", "--help")
	if err != nil || strings.Contains(out, "unavailable") || !strings.Contains(out, "rwth-vpn uninstall") {
		t.Fatalf("help accessed credentials: %v %s", err, out)
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
