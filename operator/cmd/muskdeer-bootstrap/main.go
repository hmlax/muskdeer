// Command muskdeer-bootstrap initializes an empty independent administration
// database selected by env -> Nacos. Existing databases are rejected.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gorm.io/gorm"

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
	fmt.Println("Empty Muskdeer administration database initialized")
}
func run() error {
	if len(os.Args) != 1 {
		return errors.New("muskdeer-bootstrap accepts environment configuration only")
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
	// This explicit bootstrap creates only generated framework/module models in
	// an empty database; existing deployments use versioned maintenance instead.
	return bootstrapDatabase(parent, db.Client().GormDB())
}

func bootstrapDatabase(ctx context.Context, db *gorm.DB) error {
	migrator := db.WithContext(ctx).Migrator()
	tables, err := migrator.GetTables()
	if err != nil {
		return err
	}
	if len(tables) != 0 {
		return errors.New("bootstrap requires an empty administration database")
	}
	// The framework schema creator orders generated foreign-key dependencies.
	// The empty-database guard above prevents using it as a runtime upgrade.
	return db.WithContext(ctx).Migrator().CreateTable(store.AllModels()...)
}
