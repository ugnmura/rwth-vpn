package cli

import (
	"strings"
	"testing"
)

func TestCredentialSizeLimit(t *testing.T) {
	if _, err := readSecret(strings.NewReader(strings.Repeat("x", 1024*1024+1))); err == nil {
		t.Fatal("accepted oversized credential")
	}
}
