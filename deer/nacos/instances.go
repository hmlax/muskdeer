package nacos

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	instancePath    = "/nacos/v2/ns/instance"
	beatPath        = "/nacos/v1/ns/instance/beat"
	beatSuccessCode = 10200
	grpcKind        = "grpc"
	minimumBeat     = 50 * time.Millisecond
	maximumBeat     = 5 * time.Second
)

// Registration describes one process endpoint. Its address must not be shared
// with a concurrently running publisher. Nacos does not provide fencing here.
type Registration struct {
	ID      string
	Service string
	Version string
	IP      string
	Port    uint32
}

type mutationResponse struct {
	Code int    `json:"code"`
	Data string `json:"data"`
}

type beatResponse struct {
	Code         int   `json:"code"`
	Interval     int64 `json:"clientBeatInterval"`
	LightEnabled bool  `json:"lightBeatEnabled"`
}

// Register publishes an ephemeral endpoint with typed identity metadata. Failure
// can be ambiguous; an owner must attempt deregistration on startup rollback.
func (c *Client) Register(ctx context.Context, record Registration) error {
	bounded, end, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer end()
	if err := validateRegistration(record); err != nil {
		return err
	}
	metadata, err := json.Marshal(instanceMetadata{
		ID:      record.ID,
		Kind:    grpcKind,
		Version: record.Version,
	})
	if err != nil {
		return failure(bounded, "encode registration", err)
	}
	values := c.registrationValues(record)
	values.Set("metadata", string(metadata))
	values.Set("healthy", "true")
	values.Set("enabled", "true")
	return c.mutate(bounded, http.MethodPost, values)
}

// Deregister removes this endpoint. It is idempotent for an absent endpoint;
// Nacos HTTP identity is scope/service/IP/port/cluster, not the Deer ID metadata.
func (c *Client) Deregister(ctx context.Context, record Registration) error {
	bounded, end, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer end()
	if err := validateRegistration(record); err != nil {
		return err
	}
	return c.mutate(bounded, http.MethodDelete, c.registrationValues(record))
}

// Beat renews only an existing instance with a light heartbeat. It never sends
// a full registration payload, so a missing record cannot be recreated by beat.
func (c *Client) Beat(ctx context.Context, record Registration) (time.Duration, error) {
	bounded, end, err := c.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer end()
	if err := validateRegistration(record); err != nil {
		return 0, err
	}
	data, err := c.authenticatedRequest(bounded, http.MethodPut, beatPath, url.Values{
		"namespaceId": []string{c.cfg.NamespaceId},
		"serviceName": []string{c.cfg.Group + "@@" + record.Service},
		"clusterName": []string{c.cfg.Cluster},
		"ip":          []string{record.IP},
		"port":        []string{strconv.FormatUint(uint64(record.Port), 10)},
	})
	if err != nil {
		return 0, failure(bounded, "heartbeat", err)
	}
	var response beatResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return 0, failure(bounded, "decode heartbeat", err)
	}
	if response.Code != beatSuccessCode {
		return 0, &APIError{
			Code:      response.Code,
			operation: "heartbeat",
		}
	}
	if !response.LightEnabled || response.Interval < minimumBeat.Milliseconds() || response.Interval > maximumBeat.Milliseconds() {
		return 0, errors.New("nacos requires light heartbeat support with an interval in 50..5000 milliseconds")
	}
	return time.Duration(response.Interval) * time.Millisecond, bounded.Err()
}

func (c *Client) mutate(ctx context.Context, method string, values url.Values) error {
	data, err := c.authenticatedRequest(ctx, method, instancePath, values)
	if err != nil {
		return failure(ctx, "instance mutation", err)
	}
	var response mutationResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return failure(ctx, "decode mutation", err)
	}
	if response.Code != 0 {
		return &APIError{
			Code:      response.Code,
			operation: "instance mutation",
		}
	}
	if response.Data != "ok" {
		return errors.New("nacos returned an invalid instance mutation receipt")
	}
	return ctx.Err()
}

func (c *Client) registrationValues(record Registration) url.Values {
	return url.Values{
		"namespaceId": []string{c.cfg.NamespaceId},
		"groupName":   []string{c.cfg.Group},
		"clusterName": []string{c.cfg.Cluster},
		"serviceName": []string{record.Service},
		"ip":          []string{record.IP},
		"port":        []string{strconv.FormatUint(uint64(record.Port), 10)},
		"ephemeral":   []string{"true"},
	}
}

func (c *Client) authenticatedRequest(ctx context.Context, method, path string, values url.Values) ([]byte, error) {
	token, err := c.authenticate(ctx)
	if err != nil {
		return nil, failure(ctx, "authenticate", err)
	}
	data, err := c.request(ctx, method, path, values, token.value)
	var apiError *APIError
	if errors.As(err, &apiError) && (apiError.HTTPStatus == http.StatusUnauthorized || apiError.HTTPStatus == http.StatusForbidden) {
		c.token.CompareAndSwap(token, nil)
	}
	return data, err
}

func validateRegistration(record Registration) error {
	if !validUnicastIP(record.IP) || record.Port == 0 || record.Port > 65535 {
		return errors.New("nacos registration requires a unicast IP and port in 1..65535")
	}
	if !validIdentifier(record.ID) || !validIdentifier(record.Service) || !strings.HasSuffix(record.Service, "."+grpcKind) || strings.TrimSpace(record.Version) == "" || len(record.Version) > 128 || strings.ContainsAny(record.Version, "\r\n") {
		return errors.New("nacos registration requires bounded Deer identity, gRPC service and version")
	}
	return nil
}

func validUnicastIP(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && (ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}
