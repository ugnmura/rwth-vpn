package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSavedSetupVerification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hpc       bool
		failAt    int
		wantCalls int
	}{
		{"vpn only", false, 0, 1}, {"hpc", true, 0, 3}, {"vpn failure stops hpc", true, 1, 1}, {"ssh setup failure", true, 2, 2}, {"hpc failure", true, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls [][]string
			err := testSavedSetup(tc.hpc, "test-app", func(name string, args ...string) error {
				calls = append(calls, append([]string{name}, args...))
				if len(calls) == tc.failAt {
					return errors.New("rejected")
				}
				return nil
			})
			if (err != nil) != (tc.failAt > 0) || len(calls) != tc.wantCalls {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
			if !reflect.DeepEqual(calls[0], []string{"test-app", "test-login"}) {
				t.Fatal(calls)
			}
			if len(calls) == 3 {
				args := strings.Join(calls[2], " ")
				for _, required := range []string{"ControlPath=none", "PubkeyAuthentication=no", "NumberOfPasswordPrompts=1", "claix true"} {
					if !strings.Contains(args, required) {
						t.Fatalf("fresh single-attempt SSH authentication missing %s", required)
					}
				}
			}
		})
	}
}
