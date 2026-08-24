//go:build tools

// Package tools records the exact Go generator modules. Production code must
// never import or execute this package.
package tools

import (
	_ "connectrpc.com/connect/cmd/protoc-gen-connect-go"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
