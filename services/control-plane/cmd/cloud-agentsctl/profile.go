package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const cliProfileVersion = 1

var errCLIProfile = errors.New("CLI profile is invalid")

type cliProfile struct {
	Version              int    `json:"version"`
	WebEndpoint          string `json:"webEndpoint"`
	ControlPlaneEndpoint string `json:"controlPlaneEndpoint"`
	CAFile               string `json:"caFile,omitempty"`
	Application          string `json:"application"`
	CredentialKind       string `json:"credentialKind"`
	Credential           string `json:"credential"`
	DefaultTenantID      string `json:"defaultTenantId,omitempty"`
	DefaultProjectID     string `json:"defaultProjectId,omitempty"`
}

func defaultCLIProfilePath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil || root == "" || !filepath.IsAbs(root) {
		return "", errCLIProfile
	}
	return filepath.Join(root, "cloud-agents", "profile.json"), nil
}

func createCLIProfile(path string, profile cliProfile) error {
	contents, err := encodeCLIProfile(profile)
	if err != nil {
		return err
	}
	return createPrivateCredentialFile(path, contents)
}

func readCLIProfile(path string) (cliProfile, error) {
	contents, err := readPrivateCredentialFile(path)
	if err != nil {
		return cliProfile{}, errCLIProfile
	}
	return decodeCLIProfile(contents)
}

func updateCLIProfileContext(path, tenantID, projectID string) error {
	profile, err := readCLIProfile(path)
	if err != nil {
		return err
	}
	profile.DefaultTenantID = tenantID
	profile.DefaultProjectID = projectID
	contents, err := encodeCLIProfile(profile)
	if err != nil {
		return err
	}
	return replacePrivateCredentialFile(path, contents)
}

func encodeCLIProfile(profile cliProfile) ([]byte, error) {
	if !validCLIProfile(profile) {
		return nil, errCLIProfile
	}
	contents, err := json.Marshal(profile)
	if err != nil {
		return nil, errCLIProfile
	}
	return append(contents, '\n'), nil
}

func decodeCLIProfile(contents []byte) (cliProfile, error) {
	var profile cliProfile
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return cliProfile{}, errCLIProfile
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return cliProfile{}, errCLIProfile
	}
	if !validCLIProfile(profile) {
		return cliProfile{}, errCLIProfile
	}
	return profile, nil
}

func validCLIProfile(profile cliProfile) bool {
	return profile.Version == cliProfileVersion &&
		strictCLIEndpoint(profile.WebEndpoint) &&
		strictCLIEndpoint(profile.ControlPlaneEndpoint) &&
		(profile.CAFile == "" || validCredentialPath(profile.CAFile)) &&
		(profile.Application == "admin" || profile.Application == "user") &&
		(profile.CredentialKind == "cliGrant" || profile.CredentialKind == "serviceAccount") &&
		validOpaqueCLICredential(profile.Credential) &&
		(profile.DefaultTenantID == "" || validCLIIdentifier(profile.DefaultTenantID)) &&
		(profile.DefaultProjectID == "" || profile.DefaultTenantID != "") &&
		(profile.DefaultProjectID == "" || validCLIIdentifier(profile.DefaultProjectID))
}

func strictCLIEndpoint(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	endpoint, err := url.Parse(value)
	if err != nil || endpoint == nil {
		return false
	}
	secureScheme := endpoint.Scheme == "https"
	if endpoint.Scheme == "http" {
		host := net.ParseIP(endpoint.Hostname())
		secureScheme = host != nil && host.IsLoopback()
	}
	return secureScheme && endpoint.Host != "" &&
		endpoint.User == nil && endpoint.Path == "" && endpoint.RawQuery == "" &&
		endpoint.Fragment == ""
}

func validOpaqueCLICredential(value string) bool {
	if len(value) != 43 {
		return false
	}
	for _, character := range value {
		if character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func validCLIIdentifier(value string) bool {
	if len(value) < 1 || len(value) > 128 || !asciiAlphaNumeric(value[0]) || !asciiAlphaNumeric(value[len(value)-1]) {
		return false
	}
	for index := 1; index < len(value)-1; index++ {
		character := value[index]
		if !asciiAlphaNumeric(character) && character != '.' && character != '_' && character != '~' && character != '-' {
			return false
		}
	}
	return true
}

func asciiAlphaNumeric(character byte) bool {
	return character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9'
}

func replacePrivateCredentialFile(path string, contents []byte) error {
	if !validCredentialPath(path) || len(contents) == 0 || len(contents) > maxCLICredentialFileBytes {
		return errCLICredentialFile
	}
	directory := filepath.Dir(path)
	if !privateCredentialDirectory(directory) {
		return errCLICredentialFile
	}
	before, err := os.Lstat(path)
	if err != nil || !privateCredentialFileInfo(before) {
		return errCLICredentialFile
	}
	temporary, err := os.CreateTemp(directory, ".profile-*")
	if err != nil {
		return errCLICredentialFile
	}
	temporaryPath := temporary.Name()
	completed := false
	defer func() {
		_ = temporary.Close()
		if !completed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return errCLICredentialFile
	}
	if _, err := temporary.Write(contents); err != nil {
		return errCLICredentialFile
	}
	if err := temporary.Sync(); err != nil {
		return errCLICredentialFile
	}
	if err := temporary.Close(); err != nil {
		return errCLICredentialFile
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, current) || !privateCredentialFileInfo(current) {
		return errCLICredentialFile
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errCLICredentialFile
	}
	completed = true
	if runtime.GOOS != "windows" {
		directoryHandle, err := os.Open(directory)
		if err != nil {
			return errCLICredentialFile
		}
		syncErr := directoryHandle.Sync()
		closeErr := directoryHandle.Close()
		if syncErr != nil || closeErr != nil {
			return errCLICredentialFile
		}
	}
	return nil
}
