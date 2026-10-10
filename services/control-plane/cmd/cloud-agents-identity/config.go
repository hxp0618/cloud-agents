package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const (
	maximumIdentityConfigBytes = 64 << 10
	maximumIdentitySecretBytes = 64 << 10
)

type runConfig struct {
	Listen                                  string            `json:"listen"`
	DatabaseURLFile                         string            `json:"databaseUrlFile"`
	TLSCertificateFile                      string            `json:"tlsCertificateFile"`
	TLSPrivateKeyFile                       string            `json:"tlsPrivateKeyFile"`
	Issuer                                  string            `json:"issuer"`
	AdminAudience                           string            `json:"adminAudience"`
	UserAudience                            string            `json:"userAudience"`
	SigningKeyID                            string            `json:"signingKeyId"`
	SigningPrivateKeyFile                   string            `json:"signingPrivateKeyFile"`
	CSRFKeyFile                             string            `json:"csrfKeyFile"`
	AdminWebCredentialFile                  string            `json:"adminWebCredentialFile"`
	UserWebCredentialFile                   string            `json:"userWebCredentialFile"`
	ControlPlaneCredentialFile              string            `json:"controlPlaneCredentialFile"`
	ControlPlaneURL                         string            `json:"controlPlaneUrl"`
	ControlPlaneRootCAFile                  string            `json:"controlPlaneRootCaFile"`
	ControlPlaneAuthorizationCredentialFile string            `json:"controlPlaneAuthorizationCredentialFile"`
	ProviderFlowKeyFile                     string            `json:"providerFlowKeyFile"`
	ProviderSecretFiles                     map[string]string `json:"providerSecretFiles"`
	ProviderRootCAFiles                     map[string]string `json:"providerRootCaFiles"`
}

type initializeConfig struct {
	BootstrapDatabaseURLFile string `json:"bootstrapDatabaseUrlFile"`
	Issuer                   string `json:"issuer"`
	AdminAudience            string `json:"adminAudience"`
	UserAudience             string `json:"userAudience"`
	SigningKeyID             string `json:"signingKeyId"`
	SigningPrivateKeyFile    string `json:"signingPrivateKeyFile"`
	SetupProofFile           string `json:"setupProofFile"`
	UserID                   string `json:"userId"`
	Email                    string `json:"email"`
	DisplayName              string `json:"displayName"`
	PasswordHashFile         string `json:"passwordHashFile"`
	KeyNotBefore             string `json:"keyNotBefore"`
	KeyNotAfter              string `json:"keyNotAfter"`
}

var runConfigFields = []string{
	"listen", "databaseUrlFile", "tlsCertificateFile", "tlsPrivateKeyFile", "issuer", "adminAudience", "userAudience",
	"signingKeyId", "signingPrivateKeyFile", "csrfKeyFile", "adminWebCredentialFile", "userWebCredentialFile",
	"controlPlaneCredentialFile", "controlPlaneUrl", "controlPlaneRootCaFile", "controlPlaneAuthorizationCredentialFile",
	"providerFlowKeyFile", "providerSecretFiles", "providerRootCaFiles",
}

var initializeConfigFields = []string{
	"bootstrapDatabaseUrlFile", "issuer", "adminAudience", "userAudience", "signingKeyId", "signingPrivateKeyFile", "setupProofFile", "userId", "email",
	"displayName", "passwordHashFile", "keyNotBefore", "keyNotAfter",
}

func loadRunConfig(path string) (runConfig, error) {
	data, err := readRegularFile(path, maximumIdentityConfigBytes, false)
	if err != nil {
		return runConfig{}, errors.New("identity run configuration is invalid")
	}
	fields, err := common.DecodeStrictObject(data, runConfigFields, runConfigFields)
	if err != nil {
		return runConfig{}, errors.New("identity run configuration is invalid")
	}
	var value runConfig
	if json.Unmarshal(data, &value) != nil {
		return runConfig{}, errors.New("identity run configuration is invalid")
	}
	value.ProviderSecretFiles, err = decodeReferenceFileMap(fields["providerSecretFiles"])
	if err == nil {
		value.ProviderRootCAFiles, err = decodeReferenceFileMap(fields["providerRootCaFiles"])
	}
	if err != nil || !validRunConfig(value) {
		return runConfig{}, errors.New("identity run configuration is invalid")
	}
	return value, nil
}

func loadInitializeConfig(path string) (initializeConfig, error) {
	data, err := readRegularFile(path, maximumIdentityConfigBytes, false)
	if err != nil {
		return initializeConfig{}, errors.New("identity initialization configuration is invalid")
	}
	if _, err := common.DecodeStrictObject(data, initializeConfigFields, initializeConfigFields); err != nil {
		return initializeConfig{}, errors.New("identity initialization configuration is invalid")
	}
	var value initializeConfig
	if json.Unmarshal(data, &value) != nil || !validInitializeConfig(value) {
		return initializeConfig{}, errors.New("identity initialization configuration is invalid")
	}
	return value, nil
}

func validRunConfig(value runConfig) bool {
	if !validBoundedText(value.Listen, 1, 512) || !validBoundedText(value.Issuer, 1, 512) ||
		!validBoundedText(value.AdminAudience, 1, 512) || !validBoundedText(value.UserAudience, 1, 512) ||
		!validBoundedText(value.SigningKeyID, 1, 128) || !validControlPlaneURL(value.ControlPlaneURL) {
		return false
	}
	if _, _, err := net.SplitHostPort(value.Listen); err != nil {
		return false
	}
	for _, path := range []string{
		value.DatabaseURLFile, value.TLSCertificateFile, value.TLSPrivateKeyFile, value.SigningPrivateKeyFile, value.CSRFKeyFile,
		value.AdminWebCredentialFile, value.UserWebCredentialFile, value.ControlPlaneCredentialFile, value.ControlPlaneRootCAFile,
		value.ControlPlaneAuthorizationCredentialFile, value.ProviderFlowKeyFile,
	} {
		if !validPath(path) {
			return false
		}
	}
	if value.ProviderSecretFiles == nil || value.ProviderRootCAFiles == nil || len(value.ProviderSecretFiles) > 128 || len(value.ProviderRootCAFiles) > 128 {
		return false
	}
	return true
}

func decodeReferenceFileMap(data json.RawMessage) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("invalid provider material references")
	}
	result := map[string]string{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || common.ValidateIdentifier(key, "/reference") != nil {
			return nil, errors.New("invalid provider material references")
		}
		if _, exists := result[key]; exists {
			return nil, errors.New("invalid provider material references")
		}
		var path string
		if decoder.Decode(&path) != nil || !validPath(path) {
			return nil, errors.New("invalid provider material references")
		}
		result[key] = path
		if len(result) > 128 {
			return nil, errors.New("invalid provider material references")
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("invalid provider material references")
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid provider material references")
	}
	return result, nil
}

func validInitializeConfig(value initializeConfig) bool {
	for _, path := range []string{value.BootstrapDatabaseURLFile, value.SigningPrivateKeyFile, value.SetupProofFile, value.PasswordHashFile} {
		if !validPath(path) {
			return false
		}
	}
	return validBoundedText(value.Issuer, 1, 512) && validBoundedText(value.AdminAudience, 1, 512) &&
		validBoundedText(value.UserAudience, 1, 512) && validBoundedText(value.SigningKeyID, 1, 128) &&
		validBoundedText(value.UserID, 1, 128) && validBoundedText(value.Email, 3, 254) &&
		validBoundedUnicodeText(value.DisplayName, 1, 160) && validBoundedText(value.KeyNotBefore, 1, 64) &&
		validBoundedText(value.KeyNotAfter, 1, 64)
}

func validControlPlaneURL(raw string) bool {
	if !validBoundedText(raw, 1, 2048) {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Path == "" &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}

func validPath(path string) bool {
	return validBoundedText(path, 1, 4096)
}

func validBoundedText(value string, minimum, maximum int) bool {
	return len(value) >= minimum && len(value) <= maximum && strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}

func validBoundedUnicodeText(value string, minimum, maximum int) bool {
	return common.ValidateString(value, minimum, maximum, "/value") == nil &&
		strings.TrimSpace(value) == value && !strings.ContainsRune(value, '\x00')
}

func readRegularFile(path string, maximum int64, secret bool) ([]byte, error) {
	if !validPath(path) || maximum < 1 {
		return nil, errors.New("file is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maximum || secret && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("file is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("file is invalid")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(data) < 1 || int64(len(data)) > maximum {
		return nil, errors.New("file is invalid")
	}
	return data, nil
}

func readSecretText(path string, maximum int64) (string, error) {
	data, err := readRegularFile(path, maximum, true)
	if err != nil {
		return "", errors.New("secret file is invalid")
	}
	value := string(data)
	if !validBoundedText(value, 1, int(maximum)) {
		return "", errors.New("secret file is invalid")
	}
	return value, nil
}
