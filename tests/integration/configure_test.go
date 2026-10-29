package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentConfigurePreviewAndApply(t *testing.T) {
	home := isolatedHome(t)
	profile := filepath.Join(home, "profile.json")
	secret := "private-test-password"
	writeTestFile(t, profile, `{"mode":"unencrypted","hpc":false,"credentials":{"user":"example","vpn_password":"`+secret+`","totp_secret":"JBSWY3DPEHPK3PXP"}}`, 0600)
	out, err := runBinary(t, "", "configure", "--file", profile, "--json")
	if err != nil || !strings.Contains(out, `"saved":false`) || strings.Contains(out, secret) {
		t.Fatalf("preview: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "config/rwth-vpn")); !os.IsNotExist(err) {
		t.Fatal("preview wrote configuration")
	}
	out, err = runBinary(t, "", "configure", "--file", profile, "--apply", "--json")
	if err != nil || !strings.Contains(out, `"saved":true`) || strings.Contains(out, secret) {
		t.Fatalf("apply: %v %s", err, out)
	}
	if err := os.Chmod(profile, 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := runBinary(t, "", "configure", "--file", profile); err == nil || strings.Contains(out, secret) {
		t.Fatal("accepted public input or echoed credential")
	}
}
