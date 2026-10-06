// Package vpnctl is a client for vpn-control's public API (/api/v1).
package vpnctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound    = errors.New("vpnctl: not found")
	ErrUnavailable = errors.New("vpnctl: panel unavailable")
)

type Client interface {
	LookupByTelegram(ctx context.Context, tgID int64) (*Customer, error)
	LookupByShortUUID(ctx context.Context, short string) (*Customer, error)
	GetCustomer(ctx context.Context, id int) (*Customer, error)
	CreateCustomer(ctx context.Context, in CustomerInput) (int, error)
	UpdateCustomer(ctx context.Context, id int, in CustomerInput) error
	ListTariffs(ctx context.Context) ([]Tariff, error)
	// ListCustomers returns every customer without details (no subscriptions).
	ListCustomers(ctx context.Context) ([]Customer, error)
	ListSubscriptions(ctx context.Context) ([]Subscription, error)
}

// APIError is a non-2xx answer; vpn-control sends {"message","code"}.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("vpnctl: %d %s %s", e.Status, e.Code, e.Message)
}

func (e *APIError) Is(target error) bool {
	return (target == ErrNotFound && e.Status == http.StatusNotFound) ||
		(target == ErrUnavailable && e.Status >= 500)
}

type HTTPClient struct {
	base  string
	token string
	hc    *http.Client
}

func New(baseURL, token string) *HTTPClient {
	return &HTTPClient{
		base:  strings.TrimRight(baseURL, "/") + "/api/v1",
		token: token,
		hc:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *HTTPClient) LookupByTelegram(ctx context.Context, tgID int64) (*Customer, error) {
	return c.customer(ctx, "/customers/lookup?telegram_id="+strconv.FormatInt(tgID, 10))
}

func (c *HTTPClient) LookupByShortUUID(ctx context.Context, short string) (*Customer, error) {
	return c.customer(ctx, "/customers/lookup?short_uuid="+url.QueryEscape(short))
}

func (c *HTTPClient) GetCustomer(ctx context.Context, id int) (*Customer, error) {
	return c.customer(ctx, "/customers/"+strconv.Itoa(id))
}

func (c *HTTPClient) CreateCustomer(ctx context.Context, in CustomerInput) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/customers", in, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *HTTPClient) UpdateCustomer(ctx context.Context, id int, in CustomerInput) error {
	return c.do(ctx, http.MethodPut, "/customers/"+strconv.Itoa(id), in, nil)
}

func (c *HTTPClient) ListTariffs(ctx context.Context) ([]Tariff, error) {
	var out []Tariff
	return out, c.do(ctx, http.MethodGet, "/tariffs", nil, &out)
}

func (c *HTTPClient) ListCustomers(ctx context.Context) ([]Customer, error) {
	var out []Customer
	return out, c.do(ctx, http.MethodGet, "/customers", nil, &out)
}

func (c *HTTPClient) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	var out []Subscription
	return out, c.do(ctx, http.MethodGet, "/subscriptions", nil, &out)
}

func (c *HTTPClient) customer(ctx context.Context, path string) (*Customer, error) {
	var out Customer
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var e struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return &APIError{Status: resp.StatusCode, Code: e.Code, Message: e.Message}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
