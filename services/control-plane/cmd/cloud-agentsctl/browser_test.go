package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestCLIBrowserCommandUsesNoShell(t *testing.T) {
	loginURL := "https://admin.example.test/cli/authorize?id=authorization-1"
	for _, test := range []struct {
		goos string
		name string
		args []string
	}{
		{goos: "darwin", name: "open", args: []string{loginURL}},
		{goos: "linux", name: "xdg-open", args: []string{loginURL}},
		{goos: "windows", name: "rundll32", args: []string{"url.dll,FileProtocolHandler", loginURL}},
	} {
		t.Run(test.goos, func(t *testing.T) {
			name, args, err := cliBrowserCommand(test.goos, loginURL)
			if err != nil {
				t.Fatal(err)
			}
			if name != test.name || !reflect.DeepEqual(args, test.args) {
				t.Fatalf("command = %q %#v", name, args)
			}
		})
	}
}

func TestCLIBrowserCommandRejectsUntrustedURLs(t *testing.T) {
	for _, value := range []string{
		"http://admin.example.test/cli/authorize",
		"https://user:password@admin.example.test/cli/authorize",
		"https://admin.example.test/cli/authorize#token",
		"//admin.example.test/cli/authorize",
		"not a URL",
	} {
		if _, _, err := cliBrowserCommand("darwin", value); !errors.Is(err, errCLIBrowserUnavailable) {
			t.Fatalf("URL %q error = %v", value, err)
		}
	}
	if _, _, err := cliBrowserCommand("plan9", "https://admin.example.test/cli/authorize"); !errors.Is(err, errCLIBrowserUnavailable) {
		t.Fatalf("unsupported OS error = %v", err)
	}
}
