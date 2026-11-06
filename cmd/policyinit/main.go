// Command policyinit explicitly imports database execution policies while paused.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/migrate"
	policymigrate "github.com/hmlax/muskdeer/deer/migrate/runtimepolicy"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
)

const (
	messageUsage       = "policyinit requires exactly one of -schema-only or -policies FILE"
	messageFailure     = "database execution policy maintenance failed"
	messageImported    = "Database execution policies imported; restart related services explicitly"
	messageMigrated    = "Database execution policy schema migrated; no policy or trading state was changed"
	maximumPolicyBytes = 1 << 20
)

func main() { os.Exit(command()) }

func command() int {
	schemaOnly := flag.Bool("schema-only", false, "explicitly migrate database execution policy schema")
	path := flag.String("policies", "", "reviewed RuntimePolicyImport JSON file; all trading nodes must be stopped")
	flag.Parse()
	if flag.NArg() != 0 || *schemaOnly == (*path != "") {
		fmt.Fprintln(os.Stderr, messageUsage)
		return 2
	}
	var input *configv1.RuntimePolicyImport
	if *path != "" {
		var err error
		input, err = readPolicies(*path)
		if err != nil {
			fmt.Fprintln(os.Stderr, messageFailure)
			return 2
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx, input); err != nil {
		fmt.Fprintln(os.Stderr, messageFailure)
		return 1
	}
	if input == nil {
		fmt.Println(messageMigrated)
	} else {
		fmt.Println(messageImported)
	}
	return 0
}

func run(ctx context.Context, input *configv1.RuntimePolicyImport) (result error) {
	cfg, err := deer.LoadNacosConfig(ctx, "execution")
	if err != nil {
		return err
	}
	database, err := postgres.New(ctx, cfg.Postgres, "execution-policy-maintenance")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, database.Close()) }()
	if err := migrate.CheckCurrent(ctx, database.GORM()); err != nil {
		return err
	}
	if err := policymigrate.Apply(ctx, database.GORM()); err != nil {
		return err
	}
	if input == nil {
		return nil
	}
	_, err = runtimeconfig.ImportDatabasePolicies(ctx, database.GORM(), input.Policies)
	return err
}

func readPolicies(path string) (input *configv1.RuntimePolicyImport, result error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximumPolicyBytes {
		return nil, runtimeconfig.ErrInvalidConfig
	}
	body, err := io.ReadAll(io.LimitReader(file, maximumPolicyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maximumPolicyBytes {
		return nil, runtimeconfig.ErrInvalidConfig
	}
	input = &configv1.RuntimePolicyImport{}
	if err := protojson.Unmarshal(body, input); err != nil {
		return nil, err
	}
	if len(input.Policies) == 0 || len(input.Policies) > 4 {
		return nil, runtimeconfig.ErrInvalidConfig
	}
	return input, nil
}
