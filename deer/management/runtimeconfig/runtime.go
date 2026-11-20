// Package runtimeconfig reads Nacos connection details from the environment and application configuration from Nacos.
package runtimeconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	nacosRegistry "github.com/go-kratos/kratos/contrib/registry/nacos/v2"
	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/registry"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	appCrypto "github.com/tx7do/go-utils/crypto"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"

	"github.com/hmlax/muskdeer/deer/management/serviceid"
	"github.com/hmlax/muskdeer/deer/management/tracing"
)

const (
	registryKey = "runtime.registry"
	settingsKey = "runtime.settings"
	tracingKey  = "runtime.tracing"
)

type Configuration struct {
	*conf.Bootstrap
	Runtime Settings
}

// nacosEnv reads only bootstrap connection options.
func nacosEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func merge(dst, src map[string]any) {
	for key, value := range src {
		child, childOK := value.(map[string]any)
		existing, existingOK := dst[key].(map[string]any)
		if childOK && existingOK {
			merge(existing, child)
			continue
		}
		dst[key] = value
	}
}

// Decode merges configuration-format documents, then decodes the typed bootstrap contract.
func Decode(documents ...[]byte) (*Configuration, error) {
	values := make(map[string]any)
	for _, document := range documents {
		next := make(map[string]any)
		if err := yaml.Unmarshal(document, &next); err != nil {
			return nil, fmt.Errorf("decode config YAML: %w", err)
		}
		merge(values, next)
	}
	runtimeData, err := yaml.Marshal(values["runtime"])
	if err != nil {
		return nil, fmt.Errorf("encode runtime settings: %w", err)
	}
	var settings Settings
	decoder := yaml.NewDecoder(bytes.NewReader(runtimeData))
	decoder.KnownFields(true)
	if err := decoder.Decode(&settings); err != nil {
		return nil, fmt.Errorf("decode runtime settings: %w", err)
	}
	delete(values, "runtime")
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	cfg := new(conf.Bootstrap)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(encoded, cfg); err != nil {
		return nil, fmt.Errorf("decode bootstrap config: %w", err)
	}
	return &Configuration{
		Bootstrap: cfg,
		Runtime:   settings,
	}, nil
}

func nacosParam() (vo.NacosClientParam, error) {
	if strings.TrimSpace(nacosEnv("NACOS_ADDR", "")) == "" {
		return vo.NacosClientParam{}, fmt.Errorf("NACOS_ADDR is required")
	}
	servers := make([]constant.ServerConfig, 0)
	for _, address := range strings.Split(nacosEnv("NACOS_ADDR", ""), ",") {
		if !strings.Contains(address, "://") {
			address = "http://" + address
		}
		u, err := url.Parse(strings.TrimSpace(address))
		if err != nil || u.Hostname() == "" {
			return vo.NacosClientParam{}, fmt.Errorf("invalid NACOS_ADDR")
		}
		port := uint64(8848)
		if u.Port() != "" {
			port, err = strconv.ParseUint(u.Port(), 10, 16)
			if err != nil || port == 0 {
				return vo.NacosClientParam{}, fmt.Errorf("invalid NACOS_ADDR port")
			}
		}
		servers = append(servers, constant.ServerConfig{
			IpAddr:      u.Hostname(),
			Port:        port,
			Scheme:      u.Scheme,
			ContextPath: nacosEnv("NACOS_CONTEXT_PATH", "/nacos"),
		})
	}
	cfg := &constant.ClientConfig{
		NamespaceId:         nacosEnv("NACOS_NAMESPACE_ID", ""),
		Username:            nacosEnv("NACOS_USERNAME", ""),
		Password:            nacosEnv("NACOS_PASSWORD", ""),
		TimeoutMs:           5000,
		NotLoadCacheAtStart: true,
		DisableUseSnapShot:  true,
		LogDir:              nacosEnv("NACOS_LOG_DIR", filepath.Join(os.TempDir(), "go-admin-nacos", "log")),
		CacheDir:            nacosEnv("NACOS_CACHE_DIR", filepath.Join(os.TempDir(), "go-admin-nacos", "cache")),
		LogLevel:            "warn",
	}
	return vo.NacosClientParam{
		ClientConfig:  cfg,
		ServerConfigs: servers,
	}, nil
}

// Load reads exactly server.yaml and data.yaml from Nacos. Application settings
// never fall back to local files, embedded samples or environment overrides.
func Load(name string) (*Configuration, registry.Registrar, naming_client.INamingClient, error) {
	param, err := nacosParam()
	if err != nil {
		return nil, nil, nil, err
	}
	// A fresh private startup cache also prevents SDK failover files from
	// overriding the remote documents. Naming keeps its separate normal cache.
	cacheDir, err := os.MkdirTemp("", "go-admin-nacos-startup-")
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if err := os.RemoveAll(cacheDir); err != nil {
			slog.Warn("remove Nacos startup cache", "error", err)
		}
	}()
	configParam := param
	clientConfig := *param.ClientConfig
	clientConfig.CacheDir = cacheDir
	configParam.ClientConfig = &clientConfig
	client, err := clients.NewConfigClient(configParam)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create Nacos configuration client: %w", err)
	}
	cfg, err := loadDocuments(client, name)
	client.CloseClient()
	if err != nil {
		return nil, nil, nil, err
	}
	naming, err := clients.NewNamingClient(param)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create Nacos discovery client: %w", err)
	}
	reg := nacosRegistry.New(naming, nacosRegistry.WithGroup(nacosEnv("NACOS_GROUP", "DEFAULT_GROUP")), nacosRegistry.WithCluster(nacosEnv("NACOS_CLUSTER", "DEFAULT")))
	return cfg, reg, naming, nil
}

type configGetter interface {
	GetConfig(vo.ConfigParam) (string, error)
}

func loadDocuments(client configGetter, name string) (*Configuration, error) {
	documents := make([][]byte, 0, 2)
	for _, dataID := range []string{"server.yaml", "data.yaml"} {
		content, err := client.GetConfig(vo.ConfigParam{
			DataId: dataID,
			Group:  nacosEnv("NACOS_GROUP", "DEFAULT_GROUP"),
		})
		if err != nil {
			return nil, fmt.Errorf("load Nacos %s: %w", dataID, err)
		}
		if strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("nacos %s is empty", dataID)
		}
		documents = append(documents, []byte(content))
	}
	cfg, err := Decode(documents...)
	if err != nil {
		return nil, err
	}
	if err := validate(cfg, name); err != nil {
		return nil, fmt.Errorf("invalid Nacos configuration: %w", err)
	}
	return cfg, nil
}

func validate(cfg *Configuration, name string) error {
	if err := tracing.Validate(cfg.Runtime.Tracing); err != nil {
		return err
	}
	if cfg.GetServer().GetGrpc().GetTimeout().AsDuration() <= 0 {
		return fmt.Errorf("server.grpc.timeout must be positive")
	}
	if cfg.GetData().GetRedis().GetAddr() == "" {
		return fmt.Errorf("data.redis.addr is required")
	}
	if cfg.GetData().GetRedis().GetDb() < 0 {
		return fmt.Errorf("data.redis.db must not be negative")
	}
	if cfg.GetAuthn().GetJwt() == nil {
		return fmt.Errorf("authn.jwt is required")
	}
	jwt := cfg.GetAuthn().GetJwt()
	if jwt.GetKey() == "" && jwt.GetPrivateKey() == "" && jwt.GetPublicKey() == "" {
		return fmt.Errorf("authn.jwt signing or verification key is required")
	}
	if cfg.Runtime.CryptoKey == "" {
		return fmt.Errorf("runtime.crypto_key is required")
	}
	switch name {
	case serviceid.AdminService:
		if cfg.GetServer().GetRest().GetAddr() == "" {
			return fmt.Errorf("server.rest.addr is required")
		}
	case serviceid.OperatorService:
		if cfg.GetServer().GetGrpc().GetAddr() == "" || cfg.GetData().GetDatabase().GetSource() == "" {
			return fmt.Errorf("server.grpc.addr and data.database.source are required")
		}
	default:
		return fmt.Errorf("unknown service %q", name)
	}
	return nil
}

// SettingsFromContext returns the immutable application settings loaded at startup.
func SettingsFromContext(ctx *bootstrap.Context) Settings {
	value, _ := ctx.GetValue(settingsKey)
	settings, _ := value.(Settings)
	return settings
}

// Registrar returns the shared Nacos registrar/discovery implementation, if configured.
func Registrar(ctx *bootstrap.Context) registry.Registrar {
	value, _ := ctx.GetValue(registryKey)
	reg, _ := value.(registry.Registrar)
	return reg
}

func NewContext(parent context.Context, name, version string) (*bootstrap.Context, func(), error) {
	cfg, reg, naming, err := Load(name)
	if err != nil {
		return nil, nil, err
	}
	if err := appCrypto.InitGlobalEncryptor(cfg.Runtime.CryptoKey, true); err != nil {
		naming.CloseClient()
		return nil, nil, fmt.Errorf("initialize Nacos runtime.crypto_key: %w", err)
	}
	info := &conf.AppInfo{
		AppId:      name,
		Name:       name,
		Version:    version,
		InstanceId: cfg.Runtime.InstanceID,
	}
	logger := bLogger.NewLoggerProvider(cfg.Logger, info)
	ctx := bootstrap.NewContextWithParam(parent, info, cfg.Bootstrap, logger)
	tracer, err := tracing.New(parent, cfg.Runtime.Tracing, tracing.Identity{
		Service:  "muskdeer-" + name,
		Instance: info.GetInstanceId(),
		Version:  version,
	})
	if err != nil {
		naming.CloseClient()
		return nil, nil, err
	}
	ctx.SetValue(tracingKey, tracer)
	ctx.SetValue(registryKey, reg)
	ctx.SetValue(settingsKey, cfg.Runtime)
	return ctx, func() {
		naming.CloseClient()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer cancel()
		if err := tracer.Close(shutdown); err != nil {
			logger.Error(shutdown, "tracing shutdown failed", "err", err)
		}
	}, nil
}

func Run(name, version string, initApp bootstrap.InitAppFunc) error {
	ctx, closeConfig, err := NewContext(context.Background(), name, version)
	if err != nil {
		return err
	}
	defer closeConfig()
	app, cleanup, err := initApp(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return app.Run()
}

// Options names each independently deployable process and enables Nacos registration.
func Options(ctx *bootstrap.Context) []kratos.Option {
	info := ctx.GetAppInfo()
	opts := []kratos.Option{kratos.Name(info.GetAppId()), kratos.Version(info.GetVersion()), kratos.Logger(bLogger.AsKratosLogger(ctx.GetLogger()))}
	if info.GetInstanceId() != "" {
		opts = append(opts, kratos.ID(info.GetInstanceId()))
	}
	if reg := Registrar(ctx); reg != nil {
		opts = append(opts, kratos.Registrar(reg))
	}
	return opts
}

// Endpoint uses an explicit advertised address for registration across hosts/containers.
func Endpoint(scheme, address, advertised string) (*url.URL, error) {
	if advertised != "" {
		address = advertised
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		interfaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}
		for _, network := range interfaces {
			if network.Flags&net.FlagUp == 0 || network.Flags&net.FlagLoopback != 0 {
				continue
			}
			addresses, err := network.Addrs()
			if err != nil {
				return nil, err
			}
			for _, address := range addresses {
				ip, _, err := net.ParseCIDR(address.String())
				if err == nil && ip.To4() != nil && !ip.IsLoopback() {
					host = ip.String()
					break
				}
			}
			if host != "" && host != "0.0.0.0" && host != "::" {
				break
			}
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			return nil, fmt.Errorf("cannot determine advertised IP; configure runtime advertised address in Nacos")
		}
	}
	return url.Parse(scheme + "://" + net.JoinHostPort(host, port))
}

// TracingFromContext returns the one owned provider created from Nacos settings.
func TracingFromContext(ctx *bootstrap.Context) *tracing.Runtime {
	value, _ := ctx.GetValue(tracingKey)
	result, _ := value.(*tracing.Runtime)
	return result
}
