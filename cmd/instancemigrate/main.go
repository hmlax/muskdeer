// Command instancemigrate explicitly copies an offline legacy native database
// into an empty database on the same configured PostgreSQL infrastructure.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/migrate/instancemove"
	"github.com/hmlax/muskdeer/deer/postgres"
	"google.golang.org/protobuf/proto"
)

func main() { os.Exit(command()) }
func command() int {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "instancemigrate accepts environment configuration only")
		return 2
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, 5*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "strategy instance migration failed")
		return 1
	}
	fmt.Println("Useful native data and independent strategy instances migrated and verified; source unchanged")
	return 0
}
func run(ctx context.Context) (result error) {
	cfg, err := deer.LoadNacosConfig(ctx, "execution")
	if err != nil {
		return err
	}
	targetName := os.Getenv("MUSKDEER_INSTANCE_TARGET_DATABASE")
	if targetName == "" || targetName == cfg.GetPostgres().GetDatabase() {
		return instancemove.ErrSource
	}
	for _, c := range targetName {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return instancemove.ErrSource
		}
	}
	sourceConfig := proto.CloneOf(cfg.Postgres)
	sourceConfig.MaxOpenConns, sourceConfig.MaxIdleConns = 2, 1
	targetConfig := proto.CloneOf(sourceConfig)
	targetConfig.Database = targetName
	source, err := postgres.New(ctx, sourceConfig, "instance-migration-source")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, source.Close()) }()
	target, err := postgres.New(ctx, targetConfig, "instance-migration-target")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, target.Close()) }()
	return instancemove.Move(ctx, source.GORM(), target.GORM())
}
