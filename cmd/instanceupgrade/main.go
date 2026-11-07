// Command instanceupgrade explicitly upgrades an offline instance database.
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
	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	"github.com/hmlax/muskdeer/deer/postgres"
)

func main() { os.Exit(command()) }
func command() int {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "instanceupgrade accepts environment configuration only")
		return 2
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, 5*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "strategy instance schema upgrade failed:", err)
		return 1
	}
	fmt.Println("Strategy instance schema upgraded and verified; historical identities retained")
	return 0
}
func run(ctx context.Context) (result error) {
	cfg, err := deer.LoadNacosConfig(ctx, "execution")
	if err != nil {
		return err
	}
	db, err := postgres.New(ctx, cfg.Postgres, "instance-schema-upgrade")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	if err := schemaformat.Upgrade(ctx, db.GORM()); err != nil {
		return err
	}
	return schemaformat.Check(ctx, db.GORM())
}
