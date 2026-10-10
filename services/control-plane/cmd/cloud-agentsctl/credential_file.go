package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const maxCLICredentialFileBytes = 64 << 10

var errCLICredentialFile = errors.New("CLI credential file is invalid")

func createPrivateCredentialFile(path string, contents []byte) error {
	if !validCredentialPath(path) || len(contents) == 0 || len(contents) > maxCLICredentialFileBytes {
		return errCLICredentialFile
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errCLICredentialFile
	}
	if !privateCredentialDirectory(directory) {
		return errCLICredentialFile
	}
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errCLICredentialFile
	}
	temporary, err := os.CreateTemp(directory, ".credential-*")
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
	if err := os.Link(temporaryPath, path); err != nil {
		return errCLICredentialFile
	}
	if err := os.Remove(temporaryPath); err != nil {
		_ = os.Remove(path)
		return errCLICredentialFile
	}
	completed = true
	return nil
}

func readPrivateCredentialFile(path string) ([]byte, error) {
	if !validCredentialPath(path) {
		return nil, errCLICredentialFile
	}
	before, err := os.Lstat(path)
	if err != nil || !privateCredentialFileInfo(before) {
		return nil, errCLICredentialFile
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errCLICredentialFile
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !privateCredentialFileInfo(after) {
		return nil, errCLICredentialFile
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxCLICredentialFileBytes+1))
	if err != nil || len(contents) == 0 || len(contents) > maxCLICredentialFileBytes {
		return nil, errCLICredentialFile
	}
	return contents, nil
}

func validCredentialPath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func privateCredentialDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && privateMode(info.Mode())
}

func privateCredentialFileInfo(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Size() > 0 && info.Size() <= maxCLICredentialFileBytes && privateMode(info.Mode())
}

func privateMode(mode os.FileMode) bool {
	return runtime.GOOS == "windows" || mode.Perm()&0o077 == 0
}
