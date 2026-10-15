package remnactl

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ExtendQuote is a quote for extending a subscription or addon.
type ExtendQuote struct {
	CustomerID int       `json:"customer_id"`
	Amount     float64   `json:"amount"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Monthly    float64   `json:"monthly"`
	Periods    []Period  `json:"periods"`
	Balance    float64   `json:"balance"`
}

// TariffQuote is a quote for changing to a different tariff.
type TariffQuote struct {
	OldMonthly float64    `json:"old_monthly"`
	NewMonthly float64    `json:"new_monthly"`
	ExpireAt   *time.Time `json:"expire_at"`
	Surcharge  float64    `json:"surcharge"`
}

// Allocation describes what subscription or addon service the payment covers.
type Allocation struct {
	Kind   string  `json:"kind"` // subscription | addon
	ID     int     `json:"id"`
	Months int     `json:"months"`
	Days   int     `json:"days"`
	Amount float64 `json:"amount"`
}

// PaymentInput is a manual payment to apply towards subscriptions and addons.
type PaymentInput struct {
	Amount      float64      `json:"amount"`
	Method      string       `json:"method"`
	Note        string       `json:"note"`
	Allocations []Allocation `json:"allocations"`
}

// Extension is the result of extending a subscription or addon (success or error).
type Extension struct {
	Kind   string     `json:"kind"`
	ID     int        `json:"id"`
	Title  string     `json:"title"`
	Months int        `json:"months"`
	Days   int        `json:"days"`
	Amount float64    `json:"amount"`
	From   *time.Time `json:"from"`
	To     *time.Time `json:"to"`
	OK     bool       `json:"ok"`
	Error  string     `json:"error"`
}

// PaymentResult is the response after paying.
type PaymentResult struct {
	PaymentID  int         `json:"payment_id"`
	Extensions []Extension `json:"extensions"`
	Balance    float64     `json:"balance"`
}

// ExtendInput is a request to extend a subscription or addon.
type ExtendInput struct {
	Months    int     `json:"months"`
	Days      int     `json:"days"`
	Amount    float64 `json:"amount"`
	AllowDebt bool    `json:"allow_debt"`
}

// ProvisionInput is a request to create a new subscription.
type ProvisionInput struct {
	CustomerID int     `json:"customer_id"`
	TariffID   int     `json:"tariff_id"`
	Username   string  `json:"username"`
	Label      string  `json:"label"`
	Months     int     `json:"months"`
	Days       int     `json:"days"`
	Amount     float64 `json:"amount"`
	AllowDebt  bool    `json:"allow_debt"`
}

// ConnectAddonInput is a request to add an addon to a subscription.
type ConnectAddonInput struct {
	TariffID  int     `json:"tariff_id"`
	Months    int     `json:"months"`
	Days      int     `json:"days"`
	Amount    float64 `json:"amount"`
	AllowDebt bool    `json:"allow_debt"`
}

// ChangeTariffInput is a request to change a subscription or addon's tariff.
type ChangeTariffInput struct {
	TariffID  int     `json:"tariff_id"`
	Surcharge float64 `json:"surcharge"`
}

func itemPath(kind string, id int) string {
	return "/items/" + kind + "/" + strconv.Itoa(id)
}

// QuoteExtend returns a price estimate for extending a subscription or addon for a given number of months/days.
func (c *HTTPClient) QuoteExtend(ctx context.Context, kind string, id, months, days int) (*ExtendQuote, error) {
	var out ExtendQuote
	q := url.Values{"months": {strconv.Itoa(months)}, "days": {strconv.Itoa(days)}}
	return &out, c.do(ctx, http.MethodGet, itemPath(kind, id)+"/quote?"+q.Encode(), nil, &out)
}

// Extend extends a subscription or addon by the given months/days.
func (c *HTTPClient) Extend(ctx context.Context, kind string, id int, in ExtendInput) (*Extension, error) {
	var out Extension
	return &out, c.do(ctx, http.MethodPost, itemPath(kind, id)+"/extend", in, &out)
}

// QuoteTariff returns a price estimate for changing to a different tariff.
func (c *HTTPClient) QuoteTariff(ctx context.Context, kind string, id, tariffID int) (*TariffQuote, error) {
	var out TariffQuote
	return &out, c.do(ctx, http.MethodGet, itemPath(kind, id)+"/tariff-quote?tariff_id="+strconv.Itoa(tariffID), nil, &out)
}

// ChangeTariff changes a subscription or addon to a different tariff.
func (c *HTTPClient) ChangeTariff(ctx context.Context, kind string, id int, in ChangeTariffInput) error {
	return c.do(ctx, http.MethodPost, itemPath(kind, id)+"/tariff", in, nil)
}

// Pay processes a manual payment for a customer.
func (c *HTTPClient) Pay(ctx context.Context, customerID int, in PaymentInput) (*PaymentResult, error) {
	if in.Allocations == nil {
		in.Allocations = []Allocation{}
	}
	var out PaymentResult
	return &out, c.do(ctx, http.MethodPost, "/customers/"+strconv.Itoa(customerID)+"/payments", in, &out)
}

// Provision creates a new subscription.
func (c *HTTPClient) Provision(ctx context.Context, in ProvisionInput) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/subscriptions/provision", in, &out)
	return out.ID, err
}

// ConnectAddon adds an addon to a subscription.
func (c *HTTPClient) ConnectAddon(ctx context.Context, subscriptionID int, in ConnectAddonInput) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/subscriptions/"+strconv.Itoa(subscriptionID)+"/addons", in, &out)
	return out.ID, err
}
