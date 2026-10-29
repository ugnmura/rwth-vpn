package setup

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ugnmura/rwth-vpn/internal/credentials"
)

func TestImportPreviewAndApply(t *testing.T) {
	paths, store, before := fixture(t)
	password := "new-private-password"
	plan, err := Prepare(paths, Request{Mode: "unencrypted", HPC: boolPointer(false), Credentials: map[string]*string{"vpn_password": &password}})
	if err != nil {
		t.Fatal(err)
	}
	unchanged(t, paths, before)
	output, err := json.Marshal(plan.Review)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), password) || plan.Review.Saved || !plan.Review.Ready {
		t.Fatalf("unsafe or incorrect review: %s", output)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get("vpn_password")
	if err != nil || value != password || !plan.Review.Saved {
		t.Fatal("apply did not save confirmed credentials")
	}
}

func TestImportMissingCredentialsCannotApply(t *testing.T) {
	paths := Paths{Dir: filepath.Join(t.TempDir(), "config")}
	plan, err := Prepare(paths, Request{Mode: "unencrypted"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Review.Ready || plan.Apply() == nil {
		t.Fatal("accepted incomplete setup")
	}
	store, err := credentials.New("file", filepath.Join(paths.Dir, "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("user"); err == nil {
		t.Fatal("unexpected credential")
	}
}

func TestDecodeRejectsInvalidInputWithoutEcho(t *testing.T) {
	for _, input := range []string{"null", "[]", `{"unknown":"private-value"}`, `{"credentials":`, "{} {}", strings.Repeat(" ", 1024*1024) + "{}"} {
		_, err := Decode(strings.NewReader(input))
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatal("invalid input accepted or echoed")
		}
	}
	if _, err := Decode(strings.NewReader(`{"mode":"unencrypted","hpc":false,"credentials":{"user":"example"}}`)); err != nil {
		t.Fatal(err)
	}
}
