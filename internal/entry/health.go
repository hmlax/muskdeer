package entry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

// CheckReady probes one configured local listener without constructing a Deer.
// It is suitable for a container exec probe and never follows redirects/proxies.
func CheckReady(ctx context.Context, listener *configv1.Listener) (result error) {
	if ctx == nil || listener == nil || listener.RequestTimeout.CheckValid() != nil || listener.RequestTimeout.AsDuration() <= 0 {
		return errors.New("readiness probe requires a context and configured listener")
	}
	host, port, err := net.SplitHostPort(listener.Address)
	number, portErr := strconv.ParseUint(port, 10, 16)
	if err != nil || portErr != nil || number == 0 || !net.ParseIP(host).IsLoopback() {
		return errors.New("readiness probe requires a concrete loopback TCP port")
	}
	bounded, cancel := context.WithTimeout(ctx, listener.RequestTimeout.AsDuration())
	defer cancel()
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(bounded, http.MethodGet, "http://"+listener.Address+"/readyz", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, response.Body.Close()) }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 257))
	if err != nil {
		return err
	}
	var body struct {
		Status string `json:"status"`
	}
	if response.StatusCode != http.StatusOK || len(data) > 256 || json.Unmarshal(data, &body) != nil || body.Status != "ready" {
		return errors.New("service is not ready")
	}
	return nil
}
