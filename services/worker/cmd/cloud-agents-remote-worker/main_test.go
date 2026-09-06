package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestHeartbeatLoopReconnectsWithBoundedBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	var waits []time.Duration
	err := runHeartbeatLoop(ctx, false, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("connection unavailable")
		}
		return nil
	}, func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		if len(waits) == 3 {
			return context.Canceled
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || attempts != 3 || !reflect.DeepEqual(waits, []time.Duration{time.Second, 2 * time.Second, 5 * time.Second}) {
		t.Fatalf("attempts=%d waits=%v err=%v", attempts, waits, err)
	}
}

func TestParseConfigBuildsCanonicalHeartbeat(t *testing.T) {
	value, err := parseConfig([]string{
		"--control-plane-url=https://control.example.test", "--tenant=tenant-alpha", "--project=project-alpha",
		"--enrollment=enrollment-alpha", "--incarnation=incarnation-alpha", "--certificate=/tmp/node.pem",
		"--private-key=/tmp/node-key.pem", "--server-ca=/tmp/ca.pem", "--kernel-version=6.12.1",
		"--capabilities=docker,exec,files", "--capacity-cpu-millis=4000",
		"--capacity-memory-bytes=8589934592", "--capacity-disk-bytes=42949672960", "--once",
	})
	if err != nil || !value.once || value.heartbeatRequest().ObservedState != "active" {
		t.Fatalf("config=%#v err=%v", value, err)
	}
	if _, err := parseConfig([]string{"--control-plane-url=https://control.example.test"}); err == nil {
		t.Fatal("accepted incomplete configuration")
	}
}
