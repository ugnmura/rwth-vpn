package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/ugnmura/rwth-vpn/internal/credentials"
	"github.com/ugnmura/rwth-vpn/internal/totp"
)

// Request is the agent integration format. Missing fields keep existing values;
// a null credential clears it. Secret values must arrive via a private file or stdin.
type Request struct {
	Mode        string             `json:"mode,omitempty"`
	HPC         *bool              `json:"hpc,omitempty"`
	Credentials map[string]*string `json:"credentials"`
}
type Change struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}
type Review struct {
	Mode    string   `json:"mode"`
	HPC     bool     `json:"hpc"`
	Ready   bool     `json:"ready"`
	Missing []string `json:"missing,omitempty"`
	Changes []Change `json:"changes"`
	Saved   bool     `json:"saved"`
}
type Plan struct {
	Review                    Review
	paths                     Paths
	source                    credentials.Store
	oldBackend, targetBackend string
	oldMode, oldProfile       []byte
	original, draft           map[string]*string
}

func Decode(reader io.Reader) (Request, error) {
	var request Request
	data, err := io.ReadAll(io.LimitReader(reader, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 || !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return request, errors.New("setup input must be a JSON object of at most 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, errors.New("invalid setup JSON; see 'rwth-vpn configure --help'")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return request, errors.New("setup input must contain exactly one JSON object")
	}
	return request, nil
}

// Prepare only reads current state. Review contains field names/actions, never values.
func Prepare(paths Paths, request Request) (*Plan, error) {
	plan := &Plan{paths: paths}
	var err error
	plan.oldMode, err = readOptional(filepath.Join(paths.Dir, "storage-mode"))
	if err != nil {
		return nil, err
	}
	if plan.oldMode != nil && strings.TrimSpace(string(plan.oldMode)) == "" {
		return nil, errors.New("empty stored credential mode")
	}
	plan.oldProfile, err = readOptional(filepath.Join(paths.Dir, "setup-profile"))
	if err != nil {
		return nil, err
	}
	plan.oldBackend, err = backend(paths.Dir, strings.TrimSpace(string(plan.oldMode)))
	if err != nil {
		return nil, err
	}
	plan.source, err = credentials.New(plan.oldBackend, filepath.Join(paths.Dir, "secrets"))
	if err != nil {
		return nil, err
	}
	plan.original, err = readValues(plan.source)
	if err != nil {
		return nil, err
	}
	plan.draft = copyValues(plan.original)
	known := map[string]bool{}
	for _, f := range fields {
		known[f.name] = true
	}
	for name, value := range request.Credentials {
		if !known[name] {
			return nil, errors.New("unknown credential field in setup JSON")
		}
		if value != nil && *value == "" {
			return nil, errors.New("use null to clear a credential; empty strings are not accepted")
		}
		if value != nil && strings.HasSuffix(name, "totp_secret") {
			if _, err := totp.Generate(*value, false); err != nil {
				return nil, errors.New("invalid TOTP secret in setup JSON")
			}
		}
		plan.draft[name] = value
	}
	mode := request.Mode
	if mode == "" {
		mode = strings.TrimSpace(string(plan.oldMode))
	}
	if mode == "" {
		mode = "encrypted"
		if plan.oldBackend == "file" {
			mode = "unencrypted"
		}
	}
	if mode != "encrypted" && mode != "unencrypted" {
		return nil, errors.New("mode must be encrypted or unencrypted")
	}
	plan.targetBackend, err = backend(paths.Dir, mode)
	if err != nil {
		return nil, err
	}
	hpc := strings.TrimSpace(string(plan.oldProfile)) == "hpc"
	if request.HPC != nil {
		hpc = *request.HPC
	}
	review := Review{Mode: mode, HPC: hpc, Ready: true}
	required := []string{"user", "vpn_password", "totp_secret"}
	if hpc {
		required = append(required, "hpc_password", "hpc_totp_secret")
	}
	for _, name := range required {
		if plan.draft[name] == nil {
			review.Ready = false
			review.Missing = append(review.Missing, name)
		}
	}
	for _, f := range fields {
		action := "keep"
		if !equalValue(plan.original[f.name], plan.draft[f.name]) {
			action = "replace"
			if plan.draft[f.name] == nil {
				action = "clear"
			} else if plan.original[f.name] == nil {
				action = "set"
			}
		}
		review.Changes = append(review.Changes, Change{f.name, action})
	}
	plan.Review = review
	return plan, nil
}

// Apply requires the caller's explicit final approval. It uses the same confirmed
// commit path as the human wizard, including concurrent-edit checks and rollback.
func (plan *Plan) Apply() error {
	if !plan.Review.Ready {
		return errors.New("required credentials are missing; preview lists their names")
	}
	if err := commit(plan.paths, plan.source, plan.oldBackend, plan.targetBackend, plan.Review.Mode, plan.Review.HPC, plan.oldMode, plan.oldProfile, plan.original, plan.draft); err != nil {
		return err
	}
	plan.Review.Saved = true
	return nil
}
