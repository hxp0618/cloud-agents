package main

import (
	"context"
	"testing"
)

func TestRunRejectsIncompleteOrUnknownCommands(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"unknown"},
		{"provision"},
		{"provision", "--root", "/srv/evidence", "--runner-uid", "1001"},
		{"provision", "--root", "/srv/evidence", "--runner-uid", "0", "--confirm-direct-local-mount"},
		{"provision", "--root", "/srv/evidence", "--runner-uid", "not-a-uid", "--confirm-direct-local-mount"},
		{"revoke"},
		{"revoke", "--root", "/srv/evidence", "extra"},
	} {
		if err := run(context.Background(), args); err == nil {
			t.Fatalf("args=%q accepted", args)
		}
	}
}
