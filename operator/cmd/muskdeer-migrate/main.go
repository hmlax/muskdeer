// Command muskdeer-migrate explicitly installs module schema version 1 in the
// administration database selected by env -> Nacos. Runtime performs no DDL.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	"github.com/hmlax/muskdeer/deer/management/serviceid"
	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
)

func main() {
	slog.New(slog.NewJSONHandler(os.Stderr, nil)).Info("service.build", "name", Name, "version", Version, "build", Build, "instance", ID)
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Muskdeer administration schema migration failed:", err)
		os.Exit(1)
	}
	fmt.Println("Muskdeer administration schema version 1 installed")
}
func run() error {
	if len(os.Args) != 1 {
		return errors.New("muskdeer-migrate accepts environment configuration only")
	}
	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ctx, closeConfig, err := runtimeconfig.NewContext(parent, serviceid.OperatorService, "muskdeer-schema-v1")
	if err != nil {
		return err
	}
	defer closeConfig()
	db, closeDB, err := data.NewGormClient(ctx)
	if err != nil {
		return err
	}
	defer closeDB()
	// The generated model remains the sole source of persistence fields/tags.
	// CreateTable is explicit maintenance, never startup AutoMigrate or a broad
	// migration of unrelated framework tables. Existing schemas are not rewritten.
	migrator := db.Client().GormDB().WithContext(parent).Migrator()
	if migrator.HasTable(&store.MuskdeerAccountBinding{}) {
		return errors.New("module table already exists; verify installed version before retrying")
	}
	return migrator.CreateTable(&store.MuskdeerAccountBinding{})
}
