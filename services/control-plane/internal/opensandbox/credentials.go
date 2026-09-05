package opensandbox

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

const maxCredentialBytes = 64 << 10

type CredentialDirectory struct{ path string }

func NewCredentialDirectory(path string) (*CredentialDirectory, error) {
	info, err := os.Lstat(path)
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) ||
		!info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalid
	}
	return &CredentialDirectory{path: path}, nil
}

func (directory *CredentialDirectory) Client(credentialRef string) (*Client, error) {
	if directory == nil || !platformID.MatchString(credentialRef) {
		return nil, ErrInvalid
	}
	root, err := os.OpenRoot(directory.path)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer root.Close()
	file, err := root.Open(filepath.Join(credentialRef, "opensandbox.json"))
	if err != nil {
		return nil, ErrUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxCredentialBytes {
		return nil, ErrInvalid
	}
	value, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil || len(value) > maxCredentialBytes {
		return nil, ErrInvalid
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
