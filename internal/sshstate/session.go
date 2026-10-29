// Package sshstate tracks which VPN transport an SSH control master belongs to.
package sshstate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Current identifies a live full tunnel or rootless proxy without reading secrets.
func Current(pidFile, stateFile, proxyFile string) string {
	for _, transport := range []struct{ kind, pid, state string }{{"full", pidFile, stateFile}, {"proxy", proxyFile, ""}} {
		data, err := os.ReadFile(transport.pid)
		if err != nil {
			continue
		}
		pid := strings.TrimSpace(string(data))
		if n, err := strconv.Atoi(pid); err != nil || n <= 0 {
			continue
		}
		status, err := exec.Command("ps", "-p", pid, "-o", "stat=").Output()
		if err != nil || len(strings.TrimSpace(string(status))) == 0 || strings.ContainsAny(string(status), "ZX") {
			continue
		}
		var state []byte
		if transport.state != "" {
			state, err = os.ReadFile(transport.state)
			if err != nil {
				continue
			}
		}
		return fmt.Sprintf("%s:%s:%x", transport.kind, pid, sha256.Sum256(state))
	}
	return ""
}

// Matches safely adopts masters created before tracking existed. A VPN restart
// thereafter invalidates the master even when a new tunnel is already running.
func Matches(path, session string) (bool, error) {
	if session == "" {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, Record(path, session)
	}
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) == session, nil
}

func Record(path, session string) error {
	if session == "" {
		return errors.New("no live VPN transport to associate with SSH")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ssh-session-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := fmt.Fprintln(tmp, session); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
