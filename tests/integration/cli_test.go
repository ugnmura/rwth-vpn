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
			command := `SELF="$HOME/bin/rwth-vpn"; cmd_uninstall`
			if purge {
				command += " --purge"
			}
			out, err := runLibrary(t, home, command)
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
	t.Setenv("TERM", "dumb")
	var cmd *exec.Cmd
	if runtime.GOOS == "linux" {
		cmd = exec.Command("script", "-q", "-e", "-c", `"$TEST_APP" secrets --mode unencrypted --no-hpc --plain`, "/dev/null")
	} else {
		cmd = exec.Command("script", "-q", "/dev/null", "sh", "-c", `"$TEST_APP" secrets --mode unencrypted --no-hpc --plain`)
	}
	cmd.Stdin = strings.NewReader("\n\n\n2\n")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "changes were saved, but verification failed") {
		t.Fatalf("incomplete VPN-only setup should save then fail verification: %v\n%s", err, out)
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

func TestMenuKeyboardShortcuts(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("PTY test requires script")
	}
	for _, input := range []string{"6", "jjjjj\n", "k k\n", "\x1b[B\x1b[B\x1b[B\x1b[B\x1b[B\n"} {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			isolatedHome(t)
			t.Setenv("TEST_APP", testBinary)
			t.Setenv("TERM", "xterm")
			var cmd *exec.Cmd
			if runtime.GOOS == "linux" {
				cmd = exec.Command("script", "-q", "-e", "-c", `"$TEST_APP"`, "/dev/null")
			} else {
				cmd = exec.Command("script", "-q", "/dev/null", "sh", "-c", `"$TEST_APP"`)
			}
			cmd.Stdin = strings.NewReader(input)
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "Usage:") {
				t.Fatalf("menu shortcut: %v\n%s", err, out)
			}
		})
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

func TestHelpDoesNotAccessKeyring(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(home, "no-such-bus"))
	writeTestFile(t, filepath.Join(home, "config/rwth-vpn/storage-mode"), "encrypted\n", 0600)
	out, err := runBinary(t, "", "--help")
	if err != nil || strings.Contains(out, "unavailable") || !strings.Contains(out, "rwth-vpn uninstall") {
		t.Fatalf("help accessed credentials: %v %s", err, out)
	}
}

func TestPublicParsingDoesNotLoadConfiguration(t *testing.T) {
	home := isolatedHome(t)
	writeTestFile(t, filepath.Join(home, "config/rwth-vpn/config"), "echo unexpected-config-read; exit 99\n", 0600)
	cases := []struct {
		args   []string
		status int
	}{
		{[]string{"up", "--help"}, 0},
		{[]string{"help", "secrets"}, 0},
		{[]string{"help", "--help"}, 0},
		{[]string{"secrets", "--mode=invalid"}, 2},
		{[]string{"status", "--unknown"}, 2},
		{[]string{"uninstall", "--purge", "extra"}, 2},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, err := runBinary(t, "", tc.args...)
			status := 0
			if err != nil {
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				status = exit.ExitCode()
			}
			if status != tc.status || strings.Contains(out, "unexpected-config-read") {
				t.Fatalf("exit %d, expected %d: %s", status, tc.status, out)
			}
		})
	}
}
