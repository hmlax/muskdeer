package deer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"google.golang.org/protobuf/proto"
)

const messageDatabasePolicyInNacos = "business execution parameters belong in the database, not Nacos"

// EncodeConfigPair creates two infrastructure-only YAML documents for explicit provisioning.
// Nacos bootstrap connection fields are never included in either document.
func EncodeConfigPair(cfg *configv1.ServiceConfig) ([]byte, []byte, error) {
	if cfg == nil {
		return nil, nil, errors.New("configuration is required")
	}
	if cfg.Business != nil {
		return nil, nil, errors.New(messageDatabasePolicyInNacos)
	}
	data, err := encodeYAML(&configv1.DataConfig{
		Name:            cfg.Name,
		Version:         cfg.Version,
		Postgres:        proto.CloneOf(cfg.Postgres),
		Redis:           proto.CloneOf(cfg.Redis),
		Jetstream:       proto.CloneOf(cfg.Jetstream),
		VenueConnection: proto.CloneOf(cfg.VenueConnection),
	})
	if err != nil {
		return nil, nil, err
	}
	service, err := encodeYAML(&configv1.ServiceSettings{
		Name:            cfg.Name,
		InstanceId:      cfg.InstanceId,
		Version:         cfg.Version,
		StartupTimeout:  proto.CloneOf(cfg.StartupTimeout),
		ShutdownTimeout: proto.CloneOf(cfg.ShutdownTimeout),
		Grpc:            proto.CloneOf(cfg.Grpc),
		Http:            proto.CloneOf(cfg.Http),
		Logging:         proto.CloneOf(cfg.Logging),
		Registration:    proto.CloneOf(cfg.GetNacos().GetRegistration()),
		GrpcSecurity:    proto.CloneOf(cfg.GrpcSecurity),
		GrpcClients:     proto.CloneOf(cfg.GrpcClients),
		Monitoring:      proto.CloneOf(cfg.Monitoring),
		AdminHttp:       proto.CloneOf(cfg.AdminHttp),
		DataSha256:      fmt.Sprintf("%x", sha256.Sum256(data)),
	})
	return data, service, err
}

func decodeConfigPair(ctx context.Context, data, service []byte) (*configv1.ServiceConfig, error) {
	var d configv1.DataConfig
	var s configv1.ServiceSettings
	if err := decodeYAML(ctx, service, &s); err != nil {
		return nil, err
	}
	if s.DataSha256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		return nil, configFailure("pair integrity", errors.New("data document differs from service binding"))
	}
	if err := decodeYAML(ctx, data, &d); err != nil {
		return nil, err
	}
	if d.Name == "" || d.Name != s.Name || d.Version == "" || d.Version != s.Version {
		return nil, configFailure("pair identity", errors.New("document identities differ"))
	}
	cfg := &configv1.ServiceConfig{
		Name:            s.Name,
		InstanceId:      s.InstanceId,
		Version:         s.Version,
		StartupTimeout:  s.StartupTimeout,
		ShutdownTimeout: s.ShutdownTimeout,
		Grpc:            s.Grpc,
		Http:            s.Http,
		Logging:         s.Logging,
		Postgres:        d.Postgres,
		Redis:           d.Redis,
		Jetstream:       d.Jetstream,
		VenueConnection: d.VenueConnection,
		GrpcSecurity:    s.GrpcSecurity,
		GrpcClients:     s.GrpcClients,
		Monitoring:      s.Monitoring,
		AdminHttp:       s.AdminHttp,
	}
	if s.Registration != nil {
		cfg.Nacos = &configv1.Nacos{Registration: s.Registration}
	}
	return cfg, nil
}

// LoadSplitConfig explicitly reads a directory of local examples/test inputs.
// Core process entry points exclusively use LoadNacosConfig.
func LoadSplitConfig(ctx context.Context, directory string) (*configv1.ServiceConfig, error) {
	if ctx == nil {
		return nil, configFailure("context", errors.New("configuration context is required"))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, configFailure("path", err)
	}
	directory = absolute
	read := func(name string) ([]byte, error) {
		path := filepath.Join(directory, name)
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > maximumConfigBytes {
			return nil, errors.New("configuration requires a bounded regular file")
		}
		return os.ReadFile(path)
	}
	data, err := read("data.yaml")
	if err != nil {
		return nil, configFailure("data read", err)
	}
	service, err := read("service.yaml")
	if err != nil {
		return nil, configFailure("service read", err)
	}
	cfg, err := decodeConfigPair(ctx, data, service)
	if err != nil {
		return nil, err
	}
	resolveConfigPaths(cfg, directory)
	return validateConfig(cfg)
}
