package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
			installer, err := os.ReadFile(filepath.Join(repositoryRoot, "install.sh"))
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
