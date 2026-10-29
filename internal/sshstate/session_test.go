package sshstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTransportChangeInvalidatesSSHMaster(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "session")
	if ok, err := Matches(marker, "full:1:first"); err != nil || !ok {
		t.Fatal("could not adopt existing master")
	}
	if ok, err := Matches(marker, "full:2:second"); err != nil || ok {
		t.Fatal("reused SSH master from previous VPN session")
	}
	if err := Record(marker, "proxy:3:third"); err != nil {
		t.Fatal(err)
	}
	if ok, err := Matches(marker, "proxy:3:third"); err != nil || !ok {
		t.Fatal("did not reuse current transport")
	}
	if ok, _ := Matches(marker, ""); ok {
		t.Fatal("reused master without a VPN")
	}
	info, _ := os.Stat(marker)
	if info.Mode().Perm() != 0600 {
		t.Fatal("session metadata permissions")
	}
}
