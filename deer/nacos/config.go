package nacos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

const configPath = "/nacos/v2/cs/config"

// GetConfig reads one explicitly scoped configuration without caching content.
func (c *Client) GetConfig(ctx context.Context, dataID string) ([]byte, error) {
	data, err := c.configRequest(ctx, http.MethodGet, dataID, nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Code int     `json:"code"`
		Data *string `json:"data"`
	}
	if json.Unmarshal(data, &response) != nil || response.Code != 0 || response.Data == nil {
		return nil, failure(ctx, "configuration response", errors.New("invalid configuration response"))
	}
	return []byte(*response.Data), nil
}

// PublishConfig is an explicit provisioning operation; startup loaders never call it.
func (c *Client) PublishConfig(ctx context.Context, dataID string, content []byte) error {
	if len(content) == 0 || len(content) > maxResponseBytes/2 {
		return errors.New("nacos configuration content is empty or exceeds its limit")
	}
	return c.changeConfig(ctx, http.MethodPost, dataID, content)
}

// DeleteConfig removes one explicitly named configuration, never a group or namespace.
func (c *Client) DeleteConfig(ctx context.Context, dataID string) error {
	return c.changeConfig(ctx, http.MethodDelete, dataID, nil)
}

func (c *Client) changeConfig(ctx context.Context, method, dataID string, content []byte) error {
	data, err := c.configRequest(ctx, method, dataID, content)
	if err != nil {
		return err
	}
	var response struct {
		Code int  `json:"code"`
		Data bool `json:"data"`
	}
	if json.Unmarshal(data, &response) != nil || response.Code != 0 || !response.Data {
		return failure(ctx, "configuration change", errors.New("configuration change was not acknowledged"))
	}
	return nil
}

func (c *Client) configRequest(ctx context.Context, method, dataID string, content []byte) ([]byte, error) {
	if !validIdentifier(dataID) {
		return nil, errors.New("nacos requires an explicit bounded configuration data ID")
	}
	bounded, end, err := c.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer end()
	token, err := c.authenticate(bounded)
	if err != nil {
		return nil, failure(bounded, "configuration authentication", err)
	}
	values := url.Values{
		"namespaceId": []string{c.cfg.NamespaceId},
		"group":       []string{c.cfg.Group},
		"dataId":      []string{dataID},
	}
	if method == http.MethodPost {
		values.Set("content", string(content))
		values.Set("type", "json")
	}
	data, err := c.request(bounded, method, configPath, values, token.value)
	if err != nil {
		return nil, failure(bounded, "configuration request", err)
	}
	return data, nil
}
