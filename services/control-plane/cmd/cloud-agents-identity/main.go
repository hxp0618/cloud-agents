package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const identityConfigEnvironment = "CLOUD_AGENTS_IDENTITY_CONFIG"

var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = fmt.Printf("cloud-agents-identity %s\n", version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cloud-agents-identity:", err)
		os.Exit(2)
	}
}

func execute(ctx context.Context, args []string, getenv func(string) string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("identity command is required")
	}
	switch args[0] {
	case "run":
		path, err := parseConfigPath(args[1:], getenv)
		if err != nil {
			return err
		}
		config, err := loadRunConfig(path)
		if err != nil {
			return err
		}
		return runIdentity(ctx, config)
	case "initialize":
		path, err := parseConfigPath(args[1:], getenv)
		if err != nil {
			return err
		}
		config, err := loadInitializeConfig(path)
		if err != nil {
			return err
		}
		result, err := initializeIdentity(ctx, config)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(result)
	case "hash-password":
		passwordFile, outputFile, err := parseHashPasswordArgs(args[1:])
		if err != nil {
			return err
		}
		return hashPasswordFile(passwordFile, outputFile)
	default:
		return errors.New("unknown identity command")
	}
}

func parseConfigPath(args []string, getenv func(string) string) (string, error) {
	set := flag.NewFlagSet("cloud-agents-identity", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	path := set.String("config", "", "bounded JSON configuration file")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return "", errors.New("invalid identity command configuration")
	}
	if *path == "" && getenv != nil {
		*path = getenv(identityConfigEnvironment)
	}
	if !validPath(*path) {
		return "", errors.New("identity configuration file is required")
	}
	return *path, nil
}

func parseHashPasswordArgs(args []string) (string, string, error) {
	set := flag.NewFlagSet("cloud-agents-identity hash-password", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	passwordFile := set.String("password-file", "", "password secret file")
	outputFile := set.String("output-file", "", "new Argon2id hash secret file")
	if err := set.Parse(args); err != nil || set.NArg() != 0 || !validPath(*passwordFile) || !validPath(*outputFile) || *passwordFile == *outputFile {
		return "", "", errors.New("password and output secret files are required")
	}
	return *passwordFile, *outputFile, nil
}
