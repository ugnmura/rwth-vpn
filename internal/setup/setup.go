// Package setup stages credential edits in memory until the final confirmation.
package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/ugnmura/rwth-vpn/internal/credentials"
	"github.com/ugnmura/rwth-vpn/internal/filelock"
	"github.com/ugnmura/rwth-vpn/internal/totp"
)

type UI interface {
	Choose(string, []string, int) (int, error)
	Input(string, bool) (string, error)
	Summary([]string)
}

type Options struct {
	Mode string
	HPC  *bool
}
type Result struct{ Saved, HPC bool }
type Paths struct{ Dir string }

type field struct {
	name, label string
	hidden      bool
}

var fields = []field{
	{"user", "RWTH username", false}, {"vpn_password", "VPN password", true}, {"totp_secret", "VPN TOTP secret", true},
	{"hpc_user", "HPC username (blank uses RWTH username)", false}, {"hpc_password", "HPC password", true}, {"hpc_totp_secret", "HPC TOTP secret", true},
}

// Run makes no persistent changes until the user chooses Save changes.
func Run(ui UI, paths Paths, opts Options) (Result, error) {
	originalMode, err := readOptional(filepath.Join(paths.Dir, "storage-mode"))
	if err != nil {
		return Result{}, err
	}
	if originalMode != nil && strings.TrimSpace(string(originalMode)) == "" {
		return Result{}, errors.New("empty stored credential mode; refusing to fall back")
	}
	originalProfile, err := readOptional(filepath.Join(paths.Dir, "setup-profile"))
	if err != nil {
		return Result{}, err
	}
	oldBackend, err := backend(paths.Dir, strings.TrimSpace(string(originalMode)))
	if err != nil {
		return Result{}, err
	}
	source, err := credentials.New(oldBackend, filepath.Join(paths.Dir, "secrets"))
	if err != nil {
		return Result{}, err
	}
	original, err := readValues(source)
	if err != nil {
		return Result{}, err
	}
	draft := copyValues(original)
	mode := opts.Mode
	if mode == "" {
		initial := 0
		_, fileErr := os.Stat(filepath.Join(paths.Dir, "secrets"))
		if oldBackend == "file" && (originalMode != nil || fileErr == nil) {
			initial = 1
		}
		selected, err := ui.Choose("Credential storage", []string{"Encrypted (OS keyring)", "Unencrypted (private file)"}, initial)
		if err != nil {
			return canceled(err)
		}
		mode = []string{"encrypted", "unencrypted"}[selected]
	}
	if mode != "encrypted" && mode != "unencrypted" {
		return Result{}, errors.New("mode must be encrypted or unencrypted")
	}
	targetBackend, err := backend(paths.Dir, mode)
	if err != nil {
		return Result{}, err
	}
	hpc := strings.TrimSpace(string(originalProfile)) == "hpc"
	if opts.HPC != nil {
		hpc = *opts.HPC
	} else {
		initial := 1
		if hpc {
			initial = 0
		}
		selected, err := ui.Choose("CLAIX HPC access", []string{"Enable HPC setup", "VPN only"}, initial)
		if err != nil {
			return canceled(err)
		}
		hpc = selected == 0
	}
	count := 3
	if hpc {
		count = len(fields)
	}
	ui.Summary([]string{"Enter keeps the current value. '-' clears it. Nothing is saved until confirmation.", "VPN credentials: https://idm.rwth-aachen.de/selfservice/", "HPC credentials: https://regapp.itc.rwth-aachen.de/"})
	for _, f := range fields[:count] {
		for {
			state := "unset"
			if draft[f.name] != nil {
				state = "set"
			}
			value, err := ui.Input(f.label+" ["+state+"]", f.hidden)
			if err != nil {
				return canceled(err)
			}
			if value == "" {
				break
			}
			if value == "-" {
				draft[f.name] = nil
				break
			}
			if strings.HasSuffix(f.name, "totp_secret") {
				if _, err := totp.Generate(value, false); err != nil {
					ui.Summary([]string{"Invalid TOTP secret; enter base32 text or an otpauth:// link."})
					continue
				}
			}
			v := value
			draft[f.name] = &v
			break
		}
	}
	summary := []string{"Review changes (values hidden)", "Storage: " + mode, fmt.Sprintf("HPC enabled: %t", hpc)}
	for _, f := range fields[:count] {
		action := "keep"
		if !equalValue(original[f.name], draft[f.name]) {
			action = "replace"
			if draft[f.name] == nil {
				action = "clear"
			}
		}
		summary = append(summary, f.label+": "+action)
	}
	ui.Summary(summary)
	ui.Summary([]string{"After saving, the CLI will test VPN credentials and, if enabled, HPC login."})
	confirm, err := ui.Choose("Save these changes?", []string{"Cancel — keep existing settings", "Save these changes"}, 0)
	if err != nil {
		return canceled(err)
	}
	if confirm != 1 {
		return Result{}, nil
	}
	if err := commit(paths, source, oldBackend, targetBackend, mode, hpc, originalMode, originalProfile, original, draft); err != nil {
		return Result{}, err
	}
	ui.Summary([]string{"Saved credentials and preferences."})
	if draft["user"] == nil || draft["vpn_password"] == nil || draft["totp_secret"] == nil {
		ui.Summary([]string{"VPN secrets incomplete: run setup again to finish."})
	}
	return Result{Saved: true, HPC: hpc}, nil
}

func canceled(err error) (Result, error) {
	if errors.Is(err, io.EOF) || errors.Is(err, ErrCanceled) {
		return Result{}, nil
	}
	return Result{}, err
}

var ErrCanceled = errors.New("setup canceled")

func backend(dir, mode string) (string, error) {
	switch mode {
	case "unencrypted":
		return "file", nil
	case "encrypted":
		switch runtime.GOOS {
		case "linux":
			return "secret-service", nil
		case "darwin":
			return "keychain", nil
		default:
			return "", errors.New("encrypted storage supports Linux and macOS")
		}
	case "":
		if _, err := os.Stat(filepath.Join(dir, "secrets")); err == nil {
			return "file", nil
		}
		if runtime.GOOS == "darwin" {
			return "keychain", nil
		}
		return "file", nil
	default:
		return "", errors.New("invalid stored credential mode")
	}
}

func readValues(store credentials.Store) (map[string]*string, error) {
	result := map[string]*string{}
	for _, f := range fields {
		value, err := store.Get(f.name)
		if errors.Is(err, credentials.ErrNotFound) {
			result[f.name] = nil
			continue
		}
		if err != nil {
			return nil, err
		}
		v := value
		result[f.name] = &v
	}
	return result, nil
}
func copyValues(values map[string]*string) map[string]*string {
	result := map[string]*string{}
	for k, v := range values {
		if v == nil {
			result[k] = nil
		} else {
			copy := *v
			result[k] = &copy
		}
	}
	return result
}
func equalValue(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func commit(paths Paths, source credentials.Store, oldBackend, targetBackend, mode string, hpc bool, oldMode, oldProfile []byte, original, draft map[string]*string) error {
	if err := os.MkdirAll(paths.Dir, 0700); err != nil {
		return err
	}
	lock, err := filelock.Acquire(filepath.Join(paths.Dir, "setup.lock"))
	if err != nil {
		return err
	}
	defer filelock.Release(lock)
	current, err := readValues(source)
	if err != nil {
		return err
	}
	currentMode, err := readOptional(filepath.Join(paths.Dir, "storage-mode"))
	if err != nil {
		return err
	}
	currentProfile, err := readOptional(filepath.Join(paths.Dir, "setup-profile"))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(original, current) || string(oldMode) != string(currentMode) || string(oldProfile) != string(currentProfile) {
		return errors.New("credentials or preferences changed while editing; nothing saved, rerun setup")
	}
	target, err := credentials.New(targetBackend, filepath.Join(paths.Dir, "secrets"))
	if err != nil {
		return err
	}
	previous, err := readValues(target)
	if err != nil {
		return err
	}
	if err := credentials.Apply(target, draft); err != nil {
		return err
	}
	profile := "vpn-only\n"
	if hpc {
		profile = "hpc\n"
	}
	profilePath := filepath.Join(paths.Dir, "setup-profile")
	modePath := filepath.Join(paths.Dir, "storage-mode")
	if err := writeAtomic(profilePath, []byte(profile)); err != nil {
		return errors.Join(err, credentials.Apply(target, previous))
	}
	if err := writeAtomic(modePath, []byte(mode+"\n")); err != nil {
		return errors.Join(err, restoreFile(profilePath, oldProfile), credentials.Apply(target, previous))
	}
	if oldBackend != targetBackend {
		if oldBackend == "file" {
			if err := os.Remove(filepath.Join(paths.Dir, "secrets")); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("saved new storage, but removing old credential file failed: %w", err)
			}
		} else {
			empty := map[string]*string{}
			for _, f := range fields {
				empty[f.name] = nil
			}
			if err := credentials.Apply(source, empty); err != nil {
				return fmt.Errorf("saved new storage, but deleting old keyring copies failed: %w", err)
			}
		}
	}
	return nil
}
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".setup-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
func restoreFile(path string, data []byte) error {
	if data == nil {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeAtomic(path, data)
}
