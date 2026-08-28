package server

import (
	"net/http"
	"strings"
)

func exactSingleHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	return firstExactValue(values)
}

func firstExactValue(values []string) (string, bool) {
	if len(values) != 1 || values[0] == "" || strings.TrimSpace(values[0]) != values[0] {
		return "", false
	}
	return values[0], true
}
