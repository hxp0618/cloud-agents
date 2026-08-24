// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"strconv"
)

type request struct {
	File   string `json:"file"`
	Source string `json:"source"`
}

type response struct {
	Imports []string `json:"imports"`
}

func main() {
	var input request
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("decode request: %v", err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), input.File, input.Source, parser.AllErrors|parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		fail("parse %s: %v", input.File, err)
	}

	imports := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			fail("unquote import in %s: %v", input.File, err)
		}
		imports = append(imports, path)
	}

	if err := json.NewEncoder(os.Stdout).Encode(response{Imports: imports}); err != nil {
		fail("encode response: %v", err)
	}
}

func fail(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
