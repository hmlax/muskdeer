// Command catalogimport previews or adds reviewed stopped market-maker catalogs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/catalogimport"
)

func main() {
	slog.Info("build.info", "name", Name, "version", Version, "build", Build, "instance", ID)
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (result error) {
	path := flag.String("file", "", "reviewed catalog JSON file")
	apply := flag.Bool("apply", false, "commit after preview; never enable new accounts or instances")
	flag.Parse()
	if *path == "" || flag.NArg() != 0 {
		return errors.New("catalogimport requires -file; -apply explicitly commits the previewed additions")
	}
	f, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	decoder := json.NewDecoder(io.LimitReader(f, 16*1024*1024))
	decoder.DisallowUnknownFields()
	var input catalogimport.Input
	if err := decoder.Decode(&input); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("catalog input must contain exactly one JSON document")
	}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, time.Minute)
	defer cancel()
	cfg, err := deer.LoadNacosConfig(ctx, "execution")
	if err != nil {
		return err
	}
	database, err := postgres.New(ctx, cfg.Postgres, "catalog-import")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, database.Close()) }()
	counts, err := catalogimport.Import(ctx, database.GORM(), &input, *apply)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(counts)
}
