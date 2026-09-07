package opensandbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maxCredentialBytes = 64 << 10

type CredentialDirectory struct{ paths []string }

func NewCredentialDirectory(paths ...string) (*CredentialDirectory, error) {
	if len(paths) == 0 {
		return nil, ErrInvalid
	}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) ||
			!info.IsDir() || info.Mode()&os.ModeSymlink != 0 || seen[path] {
			return nil, ErrInvalid
		}
		seen[path] = true
	}
	return &CredentialDirectory{paths: append([]string(nil), paths...)}, nil
}

func (directory *CredentialDirectory) Client(credentialRef string) (*Client, error) {
	if directory == nil || !platformID.MatchString(credentialRef) {
		return nil, ErrInvalid
	}
	var value []byte
	for _, path := range directory.paths {
		candidate, found, err := readOpenSandboxCredential(path, credentialRef)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if value != nil {
			return nil, ErrInvalid
		}
		value = candidate
	}
	if value == nil {
		return nil, ErrUnavailable
	}
	var config struct {
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"apiKey"`
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, ErrInvalid
	}
	return New(config.Endpoint, config.APIKey)
}

func readOpenSandboxCredential(path, credentialRef string) ([]byte, bool, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, false, ErrUnavailable
	}
	defer root.Close()
	var value []byte
	for _, name := range []string{filepath.Join(credentialRef, "opensandbox.json"), credentialRef + ".opensandbox.json"} {
		file, err := root.Open(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, ErrUnavailable
		}
		info, statErr := file.Stat()
		candidate, readErr := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
		closeErr := file.Close()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxCredentialBytes ||
			readErr != nil || closeErr != nil || len(candidate) > maxCredentialBytes || value != nil {
			return nil, false, ErrInvalid
		}
		value = candidate
	}
	return value, value != nil, nil
}
