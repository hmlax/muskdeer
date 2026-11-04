// Command doctor checks local protobuf configuration.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	os.Exit(code)
}

func run(ctx context.Context, args []string, output, diagnostic io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	path := flags.String("config", "", "local paired YAML directory or explicit regression JSON file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *path == "" || flags.NArg() != 0 {
		return diagnosticExit(diagnostic, "doctor requires -config and no positional arguments", 2)
	}
	cfg, err := loadConfig(ctx, *path)
	if err != nil {
		return diagnosticExit(diagnostic, err, 1)
	}
	if err := runtimeconfig.ValidateConfig(cfg); err != nil {
		return diagnosticExit(diagnostic, err, 1)
	}
	if cfg.AdminHttp != nil || cfg.Http != nil {
		return diagnosticExit(diagnostic, "management HTTP is not supported", 1)
	}
	if _, err := fmt.Fprintln(output, "configuration syntax and limits validated"); err != nil {
		return 1
	}
	return 0
}

func diagnosticExit(output io.Writer, message any, code int) int {
	if _, err := fmt.Fprintln(output, message); err != nil {
		return 1
	}
	return code
}

func loadConfig(ctx context.Context, path string) (*configv1.ServiceConfig, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return deer.LoadSplitConfig(ctx, path)
	}
	return deer.LoadConfig(ctx, path)
}
