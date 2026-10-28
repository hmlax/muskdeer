// Package adminhttp delivers management authentication through Hertz.
package adminhttp

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// MessageInvalidConfig never includes an untrusted origin or configuration.
const MessageInvalidConfig = "management HTTP requires api, PostgreSQL, a loopback listener and an explicit canonical origin"

// ErrInvalidConfig rejects incomplete management opt-in before serving requests.
var ErrInvalidConfig = errors.New(MessageInvalidConfig)

// ValidateConfig opens no resource and never trusts forwarded metadata.
func ValidateConfig(cfg *configv1.ServiceConfig) error {
	if cfg == nil {
		return ErrInvalidConfig
	}
	admin := cfg.AdminHttp
	if admin == nil {
		return nil
	}
	if !admin.Enabled {
		if admin.PublicOrigin != "" || admin.AllowInsecureLoopback {
			return ErrInvalidConfig
		}
		return nil
	}
	if cfg.Name != "api" || cfg.Postgres == nil || cfg.Http == nil {
		return ErrInvalidConfig
	}
	bindHost, bindPort, err := net.SplitHostPort(cfg.Http.Address)
	bindIP, parseErr := netip.ParseAddr(bindHost)
	if err != nil || parseErr != nil || !bindIP.IsLoopback() {
		return ErrInvalidConfig
	}
	if _, err := strconv.ParseUint(bindPort, 10, 16); err != nil {
		return ErrInvalidConfig
	}
	origin, err := parseOrigin(admin.PublicOrigin, admin.AllowInsecureLoopback)
	if err != nil {
		return err
	}
	if origin.Port() == "0" && (origin.Scheme != "http" || bindPort != "0" || origin.Hostname() != bindHost) {
		return ErrInvalidConfig
	}
	return nil
}

func parseOrigin(value string, allowInsecure bool) (*url.URL, error) {
	if len(value) == 0 || len(value) > 256 || strings.TrimSpace(value) != value || strings.ToLower(value) != value {
		return nil, ErrInvalidConfig
	}
	origin, err := url.Parse(value)
	if err != nil || !canonicalOrigin(origin, value) {
		return nil, ErrInvalidConfig
	}
	for _, character := range origin.Host {
		if character > 127 || character <= 32 {
			return nil, ErrInvalidConfig
		}
	}
	if origin.Port() != "" {
		port, err := strconv.ParseUint(origin.Port(), 10, 16)
		if err != nil || origin.Scheme == "https" && port == 0 || origin.Scheme == "https" && port == 443 || origin.Scheme == "http" && port == 80 {
			return nil, ErrInvalidConfig
		}
	}
	switch origin.Scheme {
	case "https":
		if allowInsecure {
			return nil, ErrInvalidConfig
		}
	case "http":
		ip, err := netip.ParseAddr(origin.Hostname())
		if !allowInsecure || err != nil || !ip.IsLoopback() {
			return nil, ErrInvalidConfig
		}
	default:
		return nil, ErrInvalidConfig
	}
	return origin, nil
}

func canonicalOrigin(origin *url.URL, value string) bool {
	return origin.User == nil && origin.Path == "" && origin.RawQuery == "" && !origin.ForceQuery && origin.Fragment == "" && origin.Opaque == "" && origin.Hostname() != "" && !strings.HasSuffix(origin.Host, ":") && origin.String() == value
}

// resolvedOrigin permits port zero only for explicit literal-loopback test/dev
// configuration. The replacement comes from the owned listener, never headers.
func resolvedOrigin(cfg *configv1.AdminHTTP, listener net.Addr) (string, bool, error) {
	if cfg == nil {
		return "", false, ErrInvalidConfig
	}
	origin, err := parseOrigin(cfg.PublicOrigin, cfg.AllowInsecureLoopback)
	if err != nil {
		return "", false, err
	}
	if origin.Port() == "0" {
		if listener == nil {
			return "", false, ErrInvalidConfig
		}
		host, port, err := net.SplitHostPort(listener.String())
		if err != nil || host != origin.Hostname() || port == "0" {
			return "", false, ErrInvalidConfig
		}
		origin.Host = net.JoinHostPort(host, port)
	}
	return origin.String(), origin.Scheme == "https", nil
}
