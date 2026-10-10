package main

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"runtime"
)

var errCLIBrowserUnavailable = errors.New("cannot open the CLI login page")

func openCLILoginBrowser(ctx context.Context, rawURL string) error {
	if ctx == nil {
		return errCLIBrowserUnavailable
	}
	name, arguments, err := cliBrowserCommand(runtime.GOOS, rawURL)
	if err != nil {
		return err
	}
	if err := exec.CommandContext(ctx, name, arguments...).Run(); err != nil {
		return errCLIBrowserUnavailable
	}
	return nil
}

func cliBrowserCommand(goos, rawURL string) (string, []string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", nil, errCLIBrowserUnavailable
	}
	switch goos {
	case "darwin":
		return "open", []string{rawURL}, nil
	case "linux":
		return "xdg-open", []string{rawURL}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}, nil
	default:
		return "", nil, errCLIBrowserUnavailable
	}
}
