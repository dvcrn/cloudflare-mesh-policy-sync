package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dvcrn/cloudflare-mesh-dns-sync/internal/httpclient"
)

const apiBase = "https://api.cloudflare.com/client/v4"

// Client reads WARP device registrations and manages Gateway rules in one account.
type Client struct {
	HTTP      *http.Client
	APIToken  string
	AccountID string
}

// Registration is a WARP device registration with its Mesh addresses.
type Registration struct {
	ID   string `json:"id"`
	IPv4 string `json:"virtual_ipv4"`
	IPv6 string `json:"virtual_ipv6"`
}

// Connector is a Mesh node. While connected, each connection's ClientID is the node's registration ID.
type Connector struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	InactiveAt  *time.Time `json:"conns_inactive_at"`
	Connections []struct {
		ClientID string `json:"client_id"`
	} `json:"connections"`
}

// Rule is a Gateway rule, limited to the fields the sync manages.
type Rule struct {
	ID           string       `json:"id,omitempty"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Enabled      bool         `json:"enabled"`
	Action       string       `json:"action"`
	Filters      []string     `json:"filters"`
	Traffic      string       `json:"traffic"`
	Identity     string       `json:"identity"`
	Precedence   int          `json:"precedence,omitempty"`
	RuleSettings RuleSettings `json:"rule_settings"`
}

type RuleSettings struct {
	OverrideIPs []string `json:"override_ips,omitempty"`
}

func (c *Client) Registrations(ctx context.Context) ([]Registration, error) {
	var all []Registration
	cursor := ""
	for {
		q := url.Values{"per_page": {"100"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var out struct {
			Result     []Registration `json:"result"`
			ResultInfo struct {
				Cursor string `json:"cursor"`
			} `json:"result_info"`
		}
		if err := c.do(ctx, http.MethodGet, "/devices/registrations?"+q.Encode(), nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Result...)
		if len(out.Result) < 100 || out.ResultInfo.Cursor == "" {
			return all, nil
		}
		cursor = out.ResultInfo.Cursor
	}
}

func (c *Client) Connectors(ctx context.Context) ([]Connector, error) {
	var all []Connector
	for page := 1; ; page++ {
		q := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}, "is_deleted": {"false"}}
		var out struct {
			Result []Connector `json:"result"`
		}
		if err := c.do(ctx, http.MethodGet, "/warp_connector?"+q.Encode(), nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Result...)
		if len(out.Result) < 100 {
			return all, nil
		}
	}
}

func (c *Client) Rules(ctx context.Context) ([]Rule, error) {
	var out struct {
		Result []Rule `json:"result"`
	}
	if err := c.do(ctx, http.MethodGet, "/gateway/rules", nil, &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

func (c *Client) CreateRule(ctx context.Context, r Rule) error {
	return c.do(ctx, http.MethodPost, "/gateway/rules", r, nil)
}

func (c *Client) UpdateRule(ctx context.Context, r Rule) error {
	return c.do(ctx, http.MethodPut, "/gateway/rules/"+url.PathEscape(r.ID), r, nil)
}

func (c *Client) DeleteRule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/gateway/rules/"+url.PathEscape(id), nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var buf []byte
	if body != nil {
		var err error
		if buf, err = json.Marshal(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+"/accounts/"+url.PathEscape(c.AccountID)+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return httpclient.DoJSON(c.HTTP, req, out)
}
