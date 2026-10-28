package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ugnmura/rwth-vpn/internal/credentials"
	runtimebundle "github.com/ugnmura/rwth-vpn/internal/runtime"
)

var testBinary string
var repositoryRoot string

func TestMain(m *testing.M) {
	var err error
	repositoryRoot, err = filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	dir, err := os.MkdirTemp("", "rwth-vpn-tests-")
	if err != nil {
		panic(err)
	}
	testBinary = filepath.Join(dir, "rwth-vpn")
	build := exec.Command("go", "build", "-o", testBinary, "./cmd/rwth-vpn")
	build.Dir = repositoryRoot
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
	writeTestFile(t, path, strings.TrimSuffix(runtimebundle.Script, "main \"$@\"\n"), 0600)
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

func fixtureStore(t *testing.T, path string) credentials.Store {
	t.Helper()
	store, err := credentials.New("file", path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
