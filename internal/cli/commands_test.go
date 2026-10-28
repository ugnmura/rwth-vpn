package cli

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		name       string
		args, want []string
	}{
		{"up", []string{"--split", "--user=ab123456"}, []string{"up", "--split", "--user", "ab123456"}},
		{"up", []string{"--split=false", "--full"}, []string{"up", "--full"}},
		{"secrets", []string{"--no-hpc", "--mode", "encrypted", "--unlock"}, []string{"secrets", "--mode", "encrypted", "--unlock", "--no-hpc"}},
		{"secrets", []string{"--mode=unencrypted", "--hpc"}, []string{"secrets", "--mode", "unencrypted", "--hpc"}},
		{"secrets", []string{"--where"}, []string{"secrets", "--where"}},
		{"uninstall", []string{"--purge=true"}, []string{"uninstall", "--purge"}},
		{"uninstall", []string{"--purge=false"}, []string{"uninstall"}},
		{"totp", []string{"hpc"}, []string{"totp", "hpc"}},
		{"status", nil, []string{"status"}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+strings.Join(tc.args, " "), func(t *testing.T) {
			cmd, _ := findCommand(tc.name)
			got, err := parseCommand(cmd, tc.args, io.Discard)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestRejectInvalidArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"up", []string{"--user"}},
		{"up", []string{"--split", "--full"}},
		{"up", []string{"--unknown"}},
		{"secrets", []string{"--mode", "invalid"}},
		{"secrets", []string{"--mode="}},
		{"secrets", []string{"--hpc", "--no-hpc"}},
		{"uninstall", []string{"--purge=invalid"}},
		{"status", []string{"unexpected"}},
		{"totp", []string{"vpn"}},
		{"totp", []string{"hpc", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+strings.Join(tc.args, " "), func(t *testing.T) {
			cmd, _ := findCommand(tc.name)
			if _, err := parseCommand(cmd, tc.args, io.Discard); err == nil {
				t.Fatal("accepted invalid arguments")
			}
		})
	}
}

func TestEveryCommandHasGeneratedHelp(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			var out bytes.Buffer
			_, err := parseCommand(cmd, []string{"--help"}, &out)
			if !errors.Is(err, flag.ErrHelp) || !strings.Contains(out.String(), "Usage: rwth-vpn "+cmd.name) {
				t.Fatalf("missing help: %v %s", err, out.String())
			}
			fs, _ := commandFlags(cmd, io.Discard)
			fs.VisitAll(func(f *flag.Flag) {
				if !strings.Contains(out.String(), "-"+f.Name) {
					t.Errorf("flag %s absent from generated help", f.Name)
				}
			})
		})
	}
}

func TestHelpTopics(t *testing.T) {
	for _, topic := range []string{"", "ssh", "config", "install", "troubleshooting"} {
		var out bytes.Buffer
		var args []string
		if topic != "" {
			args = []string{topic}
		}
		if err := printHelp(&out, args); err != nil || out.Len() == 0 {
			t.Fatalf("topic %q: %v %s", topic, err, out.String())
		}
	}
	if err := printHelp(io.Discard, []string{"unknown"}); err == nil {
		t.Fatal("accepted unknown help topic")
	}
}
