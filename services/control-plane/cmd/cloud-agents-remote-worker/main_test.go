package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
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
		"--private-key=/tmp/node-key.pem", "--server-ca=/tmp/ca.pem", "--state-file=/tmp/node-state.json", "--kernel-version=6.12.1",
		"--docker-endpoint=https://docker.example.test", "--credential-directory=/tmp", "--credential-ref=fixture-only",
		"--capabilities=docker,exec,files", "--capacity-cpu-millis=4000",
		"--capacity-memory-bytes=8589934592", "--capacity-disk-bytes=42949672960", "--once",
	})
	if err != nil || !value.once || value.heartbeatRequest(initialNodeState(value.incarnationID)).ObservedState != "active" {
		t.Fatalf("config=%#v err=%v", value, err)
	}
	if _, err := parseConfig([]string{"--control-plane-url=https://control.example.test"}); err == nil {
		t.Fatal("accepted incomplete configuration")
	}
	withoutDocker := []string{
		"--control-plane-url=https://control.example.test", "--tenant=tenant-alpha", "--project=project-alpha",
		"--enrollment=enrollment-alpha", "--incarnation=incarnation-alpha", "--certificate=/tmp/node.pem",
		"--private-key=/tmp/node-key.pem", "--server-ca=/tmp/ca.pem", "--state-file=/tmp/node-state.json",
		"--kernel-version=6.12.1", "--capabilities=exec,files", "--capacity-cpu-millis=4000",
		"--capacity-memory-bytes=8589934592", "--capacity-disk-bytes=42949672960",
	}
	if _, err := parseConfig(withoutDocker); err != nil {
		t.Fatalf("non-Docker heartbeat config rejected: %v", err)
	}
}

func TestRemoteWorkerCommandStateSurvivesRestartAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node", "state.json")
	state, err := loadNodeState(path, "incarnation-alpha")
	if err != nil || state.ObservedGeneration != 1 || state.ObservedState != "active" {
		t.Fatalf("initial state=%#v err=%v", state, err)
	}
	now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	command := &platform.RemoteWorkerCommand{CommandID: "command-alpha", Generation: 2, DesiredState: "drained", Deadline: now.Add(30 * time.Second).Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: "incarnation-alpha", Command: command}, now); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadNodeState(path, "incarnation-alpha")
	if err != nil || restarted.ObservedGeneration != 2 || restarted.ObservedState != "drained" || restarted.CommandReceipt == nil || restarted.CommandReceipt.Result != "succeeded" {
		t.Fatalf("restarted state=%#v err=%v", restarted, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode=%v", info.Mode().Perm())
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: "incarnation-alpha", Command: command}, now); err != nil || restarted.ObservedGeneration != 2 || restarted.CommandReceipt == nil || restarted.CommandReceipt.Result != "succeeded" {
		t.Fatalf("duplicate state=%#v err=%v", restarted, err)
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: "incarnation-alpha"}, now); err != nil || restarted.CommandReceipt != nil {
		t.Fatalf("acknowledged state=%#v err=%v", restarted, err)
	}
}

func TestRemoteWorkerRejectsExpiredCommandWithoutAdvancingGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := initialNodeState("incarnation-alpha")
	now := time.Date(2026, 9, 6, 10, 0, 31, 0, time.UTC)
	command := &platform.RemoteWorkerCommand{CommandID: "command-alpha", Generation: 2, DesiredState: "drained", Deadline: now.Add(-time.Second).Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: "incarnation-alpha", Command: command}, now); err != nil {
		t.Fatal(err)
	}
	if state.ObservedGeneration != 1 || state.ObservedState != "active" || state.CommandReceipt == nil || state.CommandReceipt.Result != "failed" || state.CommandReceipt.StableErrorCode != "remote-worker-command-expired" {
		t.Fatalf("expired state=%#v", state)
	}
}

func TestRemoteWorkerSandboxExecStateSurvivesRestartAndAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := initialNodeState("incarnation-alpha")
	command := &platform.RemoteWorkerSandboxExecCommand{CommandID: "rwexec-alpha", WorkspaceID: "workspace-alpha",
		TargetID: "target-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 3, RuntimeID: "runtime-alpha",
		RuntimeOperationID: "operation-alpha", RuntimeSpecDigest: "sha256:" + strings.Repeat("a", 64),
		Command: "printf bounded", TimeoutSeconds: 10, Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID, SandboxExecCommand: command}, time.Now()); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadNodeState(path, state.IncarnationID)
	if err != nil || restarted.SandboxExecCommand == nil || restarted.SandboxExecCommand.CommandID != command.CommandID {
		t.Fatalf("restarted state=%#v err=%v", restarted, err)
	}
	restarted.SandboxExecCommandReceipt = &platform.RemoteWorkerSandboxExecCommandReceipt{CommandID: command.CommandID,
		SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration, Result: "succeeded", ExitCode: 7,
		Stdout: "proof\n", Stderr: "failed", ExecutionTimeMillis: 1}
	if err := saveNodeState(path, restarted); err != nil {
		t.Fatal(err)
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID}, time.Now()); err != nil ||
		restarted.SandboxExecCommand != nil || restarted.SandboxExecCommandReceipt != nil {
		t.Fatalf("acknowledged state=%#v err=%v", restarted, err)
	}
}

func TestRemoteWorkerSandboxFileStateSurvivesRestartAndAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := initialNodeState("incarnation-alpha")
	command := &platform.RemoteWorkerSandboxFileCommand{CommandID: "rwfile-alpha", EventID: "file-alpha",
		GrantID: "grant-alpha", WorkspaceID: "workspace-alpha", TargetID: "target-alpha",
		SandboxID: "sandbox-alpha", SandboxGeneration: 3, RuntimeID: "runtime-alpha",
		RuntimeOperationID: "operation-alpha", RuntimeSpecDigest: "sha256:" + strings.Repeat("a", 64),
		Action: "list", Path: ".", Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := reconcileHeartbeat(path, &state, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID, SandboxFileCommand: command}, time.Now()); err != nil {
		t.Fatal(err)
	}
	restarted, err := loadNodeState(path, state.IncarnationID)
	if err != nil || restarted.SandboxFileCommand == nil || restarted.SandboxFileCommand.CommandID != command.CommandID {
		t.Fatalf("restarted state=%#v err=%v", restarted, err)
	}
	restarted.SandboxFileCommandReceipt = &platform.RemoteWorkerSandboxFileCommandReceipt{CommandID: command.CommandID,
		EventID: command.EventID, GrantID: command.GrantID, SandboxID: command.SandboxID,
		SandboxGeneration: command.SandboxGeneration, Action: command.Action, Result: "succeeded",
		List: &platform.RemoteWorkerSandboxFileListResult{Entries: []platform.SandboxFileEntry{}}}
	if err := saveNodeState(path, restarted); err != nil {
		t.Fatal(err)
	}
	if err := reconcileHeartbeat(path, &restarted, platform.RemoteWorkerHeartbeat{IncarnationID: state.IncarnationID}, time.Now()); err != nil ||
		restarted.SandboxFileCommand != nil || restarted.SandboxFileCommandReceipt != nil {
		t.Fatalf("acknowledged state=%#v err=%v", restarted, err)
	}
}
