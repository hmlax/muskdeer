package deer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/encoding/protojson"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

const maximumConfigBytes = 1024 * 1024

// ConfigError reports a safe loading stage without echoing input values. Its
// cause remains inspectable; callers must not log unwrapped configuration text.
type ConfigError struct {
	stage string
	cause error
}

func (e *ConfigError) Error() string { return "configuration " + e.stage + " failed" }

// Unwrap preserves filesystem, parsing and validation causes for inspection.
func (e *ConfigError) Unwrap() error { return e.cause }

// LoadConfig reads bounded, strict protobuf JSON from a regular file. Relative
// log/PEM paths resolve against that file's directory. It validates structural
// settings without opening referenced files, sockets, pools or log writers.
func LoadConfig(ctx context.Context, path string) (*configv1.ServiceConfig, error) {
	if ctx == nil {
		return nil, errors.New("configuration context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, configFailure("path resolution", err)
	}
	// Avoid opening ordinary directory/device/FIFO inputs as configuration.
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, configFailure("read", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maximumConfigBytes {
		return nil, configFailure("read", errors.New("configuration requires a regular file of at most 1 MiB"))
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, configFailure("read", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maximumConfigBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, configFailure("read", err)
	}
	if len(data) > maximumConfigBytes {
		return nil, configFailure("read", errors.New("configuration exceeds 1 MiB"))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := &configv1.ServiceConfig{}
	decoder := protojson.UnmarshalOptions{
		DiscardUnknown: false,
		AllowPartial:   false,
		RecursionLimit: 64,
	}
	if err := decoder.Unmarshal(data, cfg); err != nil {
		return nil, configFailure("decode", err)
	}
	resolveConfigPaths(cfg, filepath.Dir(absolute))
	validated, err := validateConfig(cfg, cfg.Http != nil)
	if err != nil {
		return nil, configFailure("validation", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return validated, nil
}

func configFailure(stage string, cause error) error {
	return &ConfigError{
		stage: stage,
		cause: cause,
	}
}

func resolveConfigPaths(cfg *configv1.ServiceConfig, base string) {
	resolve := func(path string) string {
		if path == "" || filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(base, path)
	}
	if file := cfg.Logging.GetFile(); file != nil {
		file.Filename = resolve(file.Filename)
	}
	if cfg.Postgres != nil {
		cfg.Postgres.RootCertFile = resolve(cfg.Postgres.RootCertFile)
	}
	if cfg.Redis != nil {
		cfg.Redis.RootCertFile = resolve(cfg.Redis.RootCertFile)
	}
	if cfg.Jetstream != nil {
		cfg.Jetstream.RootCertFile = resolve(cfg.Jetstream.RootCertFile)
	}
	if cfg.Nacos != nil {
		cfg.Nacos.RootCertFile = resolve(cfg.Nacos.RootCertFile)
	}
	for _, mtls := range []*configv1.MutualTLS{cfg.GrpcSecurity.GetMtls(), cfg.GrpcClients.GetMtls()} {
		if mtls == nil {
			continue
		}
		mtls.CertificateFile = resolve(mtls.CertificateFile)
		mtls.PrivateKeyFile = resolve(mtls.PrivateKeyFile)
		mtls.RootCertFile = resolve(mtls.RootCertFile)
	}
}
