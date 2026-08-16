package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/mountauthority"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cloud-agents-evidencefs-provision: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("expected provision or revoke")
	}
	switch args[0] {
	case "provision":
		set := flag.NewFlagSet("provision", flag.ContinueOnError)
		set.SetOutput(io.Discard)
		rootPath := set.String("root", "", "canonical evidence root path")
		runnerUIDRaw := set.String("runner-uid", "", "non-root runner uid")
		confirm := set.Bool("confirm-direct-local-mount", false, "confirm a dedicated direct-local ext4/xfs mount")
		if err := set.Parse(args[1:]); err != nil || set.NArg() != 0 || *rootPath == "" || *runnerUIDRaw == "" || !*confirm {
			return errors.New("invalid provision arguments")
		}
		runnerUID, err := strconv.ParseUint(*runnerUIDRaw, 10, 32)
		if err != nil || runnerUID == 0 {
			return errors.New("invalid runner uid")
		}
		return mountauthority.Provision(ctx, mountauthority.ProvisionRequest{RootPath: *rootPath, RunnerUID: uint32(runnerUID), ConfirmDirectLocalMount: true})
	case "revoke":
		set := flag.NewFlagSet("revoke", flag.ContinueOnError)
		set.SetOutput(io.Discard)
		rootPath := set.String("root", "", "canonical evidence root path")
		if err := set.Parse(args[1:]); err != nil || set.NArg() != 0 || *rootPath == "" {
			return errors.New("invalid revoke arguments")
		}
		return mountauthority.Revoke(ctx, *rootPath)
	default:
		return errors.New("expected provision or revoke")
	}
}
