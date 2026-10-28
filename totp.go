package main

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
)

type token struct {
	secret    string
	key       []byte
	digits    otp.Digits
	period    int64
	algorithm otp.Algorithm
}

func parseToken(raw string) (token, error) {
	t := token{digits: otp.DigitsSix, period: 30, algorithm: otp.AlgorithmSHA1}
	t.secret = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(t.secret), "otpauth://") {
		u, err := url.Parse(t.secret)
		if err != nil {
			return t, errors.New("invalid otpauth URI")
		}
		if u.Host != "totp" {
			return t, errors.New("only TOTP otpauth URIs are supported")
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return t, errors.New("invalid otpauth parameters")
		}
		t.secret = q.Get("secret")
		switch q.Get("digits") {
		case "", "6":
		case "8":
			t.digits = otp.DigitsEight
		default:
			return t, errors.New("TOTP digits must be 6 or 8")
		}
		if p := q.Get("period"); p != "" {
			n, err := strconv.ParseInt(p, 10, 64)
			if err != nil || n < 5 || n > 86400 {
				return t, errors.New("TOTP period must be between 5 and 86400 seconds")
			}
			t.period = n
		}
		switch strings.ToUpper(q.Get("algorithm")) {
		case "", "SHA1":
		case "SHA256":
			t.algorithm = otp.AlgorithmSHA256
		case "SHA512":
			t.algorithm = otp.AlgorithmSHA512
		default:
			return t, errors.New("unsupported TOTP algorithm")
		}
	}
	t.secret = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(t.secret))
	if t.secret == "" {
		return t, errors.New("empty TOTP secret")
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(t.secret, "="))
	if err != nil {
		return t, errors.New("invalid base32 TOTP secret")
	}
	t.key = key
	return t, nil
}
func (t token) code(now time.Time) (string, error) {
	return hotp.GenerateCodeCustom(t.secret, uint64(now.Unix()/t.period), hotp.ValidateOpts{Digits: t.digits, Algorithm: t.algorithm})
}
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func unlockFile(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }
func (t token) freshTime() (time.Time, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return time.Time{}, err
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "rwth-vpn")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return time.Time{}, err
	}
	hash := sha256.Sum256(t.key)
	tag := hex.EncodeToString(hash[:])[:12]
	f, err := lockFile(filepath.Join(dir, "totp-"+tag+".last"))
	if err != nil {
		return time.Time{}, err
	}
	defer unlockFile(f)
	data, err := io.ReadAll(f)
	if err != nil {
		return time.Time{}, err
	}
	last := int64(-1)
	if len(strings.TrimSpace(string(data))) > 0 {
		last, err = strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return time.Time{}, errors.New("invalid TOTP replay state")
		}
	}
	now := time.Now()
	// A corrupt/future timestamp must not cause an unbounded wait.
	if last > now.Unix()/t.period {
		return time.Time{}, errors.New("TOTP replay state is in the future; check the system clock")
	}
	for t.period-now.Unix()%t.period < 5 || now.Unix()/t.period <= last {
		time.Sleep(time.Duration(t.period-now.Unix()%t.period)*time.Second + 200*time.Millisecond)
		now = time.Now()
	}
	if err := f.Truncate(0); err != nil {
		return time.Time{}, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return time.Time{}, err
	}
	if _, err := fmt.Fprint(f, now.Unix()/t.period); err != nil {
		return time.Time{}, err
	}
	return now, f.Sync()
}
func printTOTP(r io.Reader, w io.Writer, fresh bool) error {
	raw, err := readSecret(r)
	if err != nil {
		return err
	}
	t, err := parseToken(raw)
	if err != nil {
		return err
	}
	now := time.Now()
	if fresh {
		now, err = t.freshTime()
		if err != nil {
			return err
		}
	}
	code, err := t.code(now)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, code)
	return err
}
