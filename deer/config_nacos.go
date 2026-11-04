package deer

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

const nacosPasswordEnvironment = "NACOS_PASSWORD"

// LoadNacosConfig loads an immutable startup snapshot using environment-only
// Nacos connection settings. An empty expectedName requires both explicit document IDs and
// is reserved for independent maintenance/readiness tools.
func LoadNacosConfig(ctx context.Context, expectedName string) (result *configv1.ServiceConfig, resultErr error) {
	if ctx == nil {
		return nil, configFailure("bootstrap", errors.New("configuration context is required"))
	}
	connection, dataID, serviceID, err := nacosEnvironment(expectedName)
	if err != nil {
		return nil, configFailure("bootstrap", err)
	}
	bounded, cancel := context.WithTimeout(ctx, connection.ConnectTimeout.AsDuration())
	defer cancel()
	source, err := nacos.NewConfigClient(bounded, connection)
	if err != nil {
		return nil, configFailure("source", err)
	}
	defer func() { resultErr = errors.Join(resultErr, source.Close()) }()
	service, err := source.GetConfig(bounded, serviceID)
	if err != nil {
		return nil, configFailure("service fetch", err)
	}
	data, err := source.GetConfig(bounded, dataID)
	if err != nil {
		return nil, configFailure("data fetch", err)
	}
	cfg, err := decodeConfigPair(bounded, data, service)
	if err != nil {
		return nil, err
	}
	if expectedName != "" && cfg.Name != expectedName {
		return nil, configFailure("identity", errors.New("configuration belongs to another service"))
	}
	registration := proto.CloneOf(cfg.GetNacos().GetRegistration())
	cfg.Nacos = proto.CloneOf(connection)
	cfg.Nacos.Registration = registration
	validated, err := validateConfig(cfg, cfg.Http != nil)
	if err != nil {
		return nil, configFailure("validation", err)
	}
	return validated, nil
}

func nacosEnvironment(service string) (*configv1.Nacos, string, string, error) {
	if os.Getenv("NACOS_DATA_ID") != "" {
		return nil, "", "", errors.New("single-document configuration is unsupported")
	}
	dataID, serviceID := os.Getenv("NACOS_DATA_CONFIG_ID"), os.Getenv("NACOS_SERVICE_CONFIG_ID")
	if dataID == "" && service != "" {
		dataID = service + ".data.yaml"
	}
	if serviceID == "" && service != "" {
		serviceID = service + ".service.yaml"
	}
	if dataID == "" || serviceID == "" || dataID == serviceID || os.Getenv("NACOS_ADDRESS") == "" || os.Getenv("NACOS_NAMESPACE") == "" ||
		os.Getenv("NACOS_GROUP") == "" || os.Getenv("NACOS_CLUSTER") == "" ||
		os.Getenv("NACOS_USERNAME") == "" || os.Getenv(nacosPasswordEnvironment) == "" {
		return nil, "", "", errors.New("explicit Nacos connection environment is required")
	}
	connect, err := nacosDuration("NACOS_CONNECT_TIMEOUT", 5*time.Second)
	if err != nil {
		return nil, "", "", err
	}
	command, err := nacosDuration("NACOS_COMMAND_TIMEOUT", 2*time.Second)
	if err != nil {
		return nil, "", "", err
	}
	tls := true
	if value := os.Getenv("NACOS_TLS_ENABLED"); value != "" {
		tls, err = strconv.ParseBool(value)
		if err != nil {
			return nil, "", "", errors.New("nacos TLS environment is invalid")
		}
	}
	return &configv1.Nacos{
		Address:        os.Getenv("NACOS_ADDRESS"),
		NamespaceId:    os.Getenv("NACOS_NAMESPACE"),
		Group:          os.Getenv("NACOS_GROUP"),
		Cluster:        os.Getenv("NACOS_CLUSTER"),
		Username:       os.Getenv("NACOS_USERNAME"),
		PasswordEnv:    nacosPasswordEnvironment,
		TlsEnabled:     proto.Bool(tls),
		RootCertFile:   os.Getenv("NACOS_ROOT_CERT_FILE"),
		ConnectTimeout: durationpb.New(connect),
		CommandTimeout: durationpb.New(command),
		MaxInflight:    8,
	}, dataID, serviceID, nil
}

func nacosDuration(name string, fallback time.Duration) (time.Duration, error) {
	if value := os.Getenv(name); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration < time.Millisecond || duration > 30*time.Second {
			return 0, errors.New("nacos timeout environment is invalid")
		}
		return duration, nil
	}
	return fallback, nil
}
