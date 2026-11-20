package main

import (
	"log/slog"
	"os"

	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	"github.com/hmlax/muskdeer/deer/management/serviceid"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
)

func newApp(ctx *bootstrap.Context, server *grpc.Server) *kratos.App {
	options := runtimeconfig.Options(ctx)
	options = append(options, kratos.Server(server))
	return kratos.New(options...)
}

func runApp() error { return runtimeconfig.Run(serviceid.OperatorService, Version, initApp) }
func main() {
	slog.New(slog.NewJSONHandler(os.Stderr, nil)).Info("service.build", "name", Name, "version", Version, "build", Build, "instance", ID)
	if err := runApp(); err != nil {
		panic(err)
	}
}
