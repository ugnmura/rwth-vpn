package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ugnmura/rwth-vpn/internal/setup"
)

func configure(args []string) int {
	fs, opts := commandFlags(command{name: "configure", summary: "Preview or apply an agent-provided setup profile"}, os.Stderr)
	if err := fs.Parse(args); err != nil {
		return report(err, 2)
	}
	if opts.inputFile == "" && !opts.stdin {
		return report(errors.New("use --file PATH or --stdin; secret values are never command-line options"), 2)
	}
	var input io.Reader = os.Stdin
	if opts.inputFile != "" {
		file, err := os.Open(opts.inputFile)
		if err != nil {
			return report(err, 1)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return report(err, 1)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return report(errors.New("setup input must be a regular private file (0600 or 0400)"), 1)
		}
		input = file
	}
	request, err := setup.Decode(input)
	if err != nil {
		return report(err, 2)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return report(err, 1)
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	plan, err := setup.Prepare(setup.Paths{Dir: filepath.Join(base, "rwth-vpn")}, request)
	if err != nil {
		return report(err, 1)
	}
	if opts.apply {
		if err := plan.Apply(); err != nil {
			return report(err, 1)
		}
	}
	if opts.json {
		if err := json.NewEncoder(os.Stdout).Encode(plan.Review); err != nil {
			return report(err, 1)
		}
	} else {
		fmt.Fprintf(os.Stdout, "Storage: %s\nHPC: %t\nReady: %t\nSaved: %t\n", plan.Review.Mode, plan.Review.HPC, plan.Review.Ready, plan.Review.Saved)
		for _, change := range plan.Review.Changes {
			fmt.Fprintf(os.Stdout, "%s: %s\n", change.Name, change.Action)
		}
		for _, name := range plan.Review.Missing {
			fmt.Fprintln(os.Stdout, "Missing:", name)
		}
		if !opts.apply {
			fmt.Fprintln(os.Stdout, "Preview only. Apply with --apply only after the human confirms this summary.")
		}
	}
	return 0
}
