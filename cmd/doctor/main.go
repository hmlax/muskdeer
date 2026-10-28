// Command doctor checks the exact compiler and local protobuf configuration.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/internal/business"
	"github.com/hmlax/muskdeer/internal/delivery/adminhttp"
)

const requiredGoVersion = "go1.27.3"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	os.Exit(code)
}

func run(ctx context.Context, args []string, output, diagnostic io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	path := flags.String("config", "", "local protobuf JSON configuration file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *path == "" || flags.NArg() != 0 {
		return diagnosticExit(diagnostic, "doctor requires -config and no positional arguments", 2)
	}
	if runtime.Version() != requiredGoVersion {
		return diagnosticExit(diagnostic, "doctor requires Go 1.27.3", 1)
	}
	cfg, err := deer.LoadConfig(ctx, *path)
	if err != nil {
		return diagnosticExit(diagnostic, err, 1)
	}
	if err := business.ValidateConfig(cfg); err != nil {
		return diagnosticExit(diagnostic, err, 1)
	}
	if err := adminhttp.ValidateConfig(cfg); err != nil {
		return diagnosticExit(diagnostic, err, 1)
	}
	if _, err := fmt.Fprintln(output, "go1.27.3: configuration syntax and limits validated"); err != nil {
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
