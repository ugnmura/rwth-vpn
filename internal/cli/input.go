package cli

import (
	"errors"
	"io"
)

func readSecret(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1024*1024+1))
	if len(data) > 1024*1024 {
		return "", errors.New("credential exceeds 1 MiB")
	}
	return string(data), err
}
