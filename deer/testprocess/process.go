// Package testprocess selects independently built application and module fixtures.
package testprocess

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// Command never accepts a role switch. Every process has its own executable.
func Command(ctx context.Context, name, path string) (*exec.Cmd, func() error, error) {
	switch name {
	case "market", "strategy", "execution", "portfolio", "operator", "cron":
	default:
		return nil, nil, errors.New("unsupported test process")
	}
	binary := name
	if name != "operator" && os.Getenv("MUSKDEER_CORE_TEST_BINARIES") != "1" {
		binary = "module-" + name
	}
	if os.Getenv("MUSKDEER_CORE_TEST_BINARIES") != "1" || name == "operator" {
		return exec.CommandContext(ctx, filepath.Join("..", "..", ".cache", "bin", binary), "-config", path), func() error { return nil }, nil
	}
	cfg, err := deer.LoadConfig(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	source, err := nacos.NewConfigClient(ctx, cfg.Nacos)
	if err != nil {
		return nil, nil, err
	}
	remote := proto.CloneOf(cfg)
	remote.Nacos = &configv1.Nacos{Registration: proto.CloneOf(cfg.Nacos.Registration)}
	data, err := protojson.Marshal(remote)
	dataID := name + "-" + rand.Text() + ".json"
	cleanup := func() error {
		bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return errors.Join(source.DeleteConfig(bounded, dataID), source.Close())
	}
	published := false
	if err == nil {
		err = source.PublishConfig(ctx, dataID, data)
		published = err == nil
	}
	if err == nil {
		visible, cancel := context.WithTimeout(ctx, cfg.Nacos.CommandTimeout.AsDuration())
		err = waitPublishedConfig(visible, source, dataID, data)
		cancel()
	}
	if err != nil {
		if published {
			return nil, nil, errors.Join(err, cleanup())
		}
		return nil, nil, errors.Join(err, source.Close())
	}
	command := exec.CommandContext(ctx, filepath.Join("..", "..", ".cache", "bin", binary))
	command.Env = append(os.Environ(),
		"NACOS_ADDRESS="+cfg.Nacos.Address, "NACOS_NAMESPACE="+cfg.Nacos.NamespaceId,
		"NACOS_GROUP="+cfg.Nacos.Group, "NACOS_CLUSTER="+cfg.Nacos.Cluster,
		"NACOS_USERNAME="+cfg.Nacos.Username, "NACOS_PASSWORD="+os.Getenv(cfg.Nacos.PasswordEnv),
		"NACOS_TLS_ENABLED="+strconv.FormatBool(cfg.Nacos.TlsEnabled == nil || cfg.Nacos.GetTlsEnabled()),
		"NACOS_ROOT_CERT_FILE="+cfg.Nacos.RootCertFile, "NACOS_DATA_ID="+dataID,
		"NACOS_CONNECT_TIMEOUT="+cfg.Nacos.ConnectTimeout.AsDuration().String(),
		"NACOS_COMMAND_TIMEOUT="+cfg.Nacos.CommandTimeout.AsDuration().String())
	return command, cleanup, nil
}

type configReader interface {
	GetConfig(context.Context, string) ([]byte, error)
}

// A publish acknowledgement precedes Nacos's asynchronous read-cache update.
// Wait only for a missing new key; other failures and unexpected content fail.
func waitPublishedConfig(ctx context.Context, source configReader, dataID string, expected []byte) error {
	ticks := time.NewTicker(10 * time.Millisecond)
	defer ticks.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		observed, err := source.GetConfig(ctx, dataID)
		if err == nil {
			if !bytes.Equal(observed, expected) {
				return errors.New("published test configuration differs on readback")
			}
			return nil
		}
		var apiError *nacos.APIError
		if !errors.As(err, &apiError) || apiError.HTTPStatus != http.StatusNotFound {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticks.C:
		}
	}
}
