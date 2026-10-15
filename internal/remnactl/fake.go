package remnactl

import (
	"context"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Fake is an in-memory Client that behaves like remna-control's lookup rules.
type Fake struct {
	mu            sync.Mutex
	customers     map[int]*Customer
	nextID        int
	Tariffs       []Tariff
	Creates       []CustomerInput
	Updates       []Update
	Err           error // returned by every call when set
	Quotes        map[string]ExtendQuote
	TariffQuotes  map[string]TariffQuote
	Payments      []PaymentInput
	Extends       []ExtendCall
	Provisions    []ProvisionInput
	AddonCalls    []AddonCall
	TariffChanges []TariffCall
	FailExtend    string
	FailStep      error
	Currency      string // the panel's base currency; "RUB" when empty
	nextPaymentID int
}

type Update struct {
	ID int
	In CustomerInput
}

type ExtendCall struct {
	Kind string
	ID   int
	In   ExtendInput
}

type AddonCall struct {
	SubscriptionID int
	In             ConnectAddonInput
}

type TariffCall struct {
	Kind string
	ID   int
	In   ChangeTariffInput
}

var _ Client = (*Fake)(nil)

func NewFake() *Fake { return &Fake{customers: map[int]*Customer{}, nextID: 1} }

// Add stores c (assigning an id when zero) and returns the stored copy.
func (f *Fake) Add(c Customer) *Customer {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == 0 {
		c.ID = f.nextID
	}
	if c.ID >= f.nextID {
		f.nextID = c.ID + 1
	}
	f.customers[c.ID] = &c
	return f.copy(&c)
}

func (f *Fake) copy(c *Customer) *Customer { cp := *c; return &cp }

// find mirrors LookupCustomer: active customers first, then the oldest.
func (f *Fake) find(match func(*Customer) bool) (*Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	ids := make([]int, 0, len(f.customers))
	for id := range f.customers {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var archived *Customer
	for _, id := range ids {
		c := f.customers[id]
		if !match(c) {
			continue
		}
		if !c.Archived {
			return f.copy(c), nil
		}
		if archived == nil {
			archived = c
		}
	}
	if archived != nil {
		return f.copy(archived), nil
	}
	return nil, &APIError{Status: 404, Code: "customer.not_found"}
}

func (f *Fake) LookupByTelegram(_ context.Context, tg int64) (*Customer, error) {
	return f.find(func(c *Customer) bool { return c.TelegramID != nil && *c.TelegramID == tg })
}

func (f *Fake) LookupByShortUUID(_ context.Context, short string) (*Customer, error) {
	return f.find(func(c *Customer) bool {
		for _, s := range c.Subscriptions {
			if s.RwUser != nil && s.RwUser.ShortUUID == short {
				return true
			}
			for _, a := range s.Addons {
				if a.RwUser != nil && a.RwUser.ShortUUID == short {
					return true
				}
			}
		}
		return false
	})
}

func (f *Fake) GetCustomer(_ context.Context, id int) (*Customer, error) {
	return f.find(func(c *Customer) bool { return c.ID == id })
}

func (f *Fake) CreateCustomer(_ context.Context, in CustomerInput) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	f.Creates = append(f.Creates, in)
	c := &Customer{ID: f.nextID}
	f.nextID++
	apply(c, in)
	f.customers[c.ID] = c
	return c.ID, nil
}

func (f *Fake) UpdateCustomer(_ context.Context, id int, in CustomerInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	c, ok := f.customers[id]
	if !ok {
		return &APIError{Status: 404}
	}
	f.Updates = append(f.Updates, Update{ID: id, In: in})
	apply(c, in)
	return nil
}

func (f *Fake) ListTariffs(context.Context) ([]Tariff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Tariffs, f.Err
}

func apply(c *Customer, in CustomerInput) {
	c.Name, c.Contact, c.TelegramID, c.Notes = in.Name, in.Contact, in.TelegramID, in.Notes
	c.ReferrerID, c.ReferralPercent, c.Archived = in.ReferrerID, in.ReferralPercent, in.Archived
}

func (f *Fake) ListCustomers(context.Context) ([]Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	ids := make([]int, 0, len(f.customers))
	for id := range f.customers {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]Customer, 0, len(ids))
	for _, id := range ids {
		c := *f.customers[id]
		c.Subscriptions = nil
		out = append(out, c)
	}
	return out, nil
}

// ListSubscriptions returns every customer's subscriptions with CustomerID set.
func (f *Fake) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var out []Subscription
	for _, c := range f.customers {
		for _, s := range c.Subscriptions {
			s.CustomerID = c.ID
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func itemKey(kind string, id int) string { return kind + ":" + strconv.Itoa(id) }

func (f *Fake) QuoteExtend(_ context.Context, kind string, id, months, days int) (*ExtendQuote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	q, ok := f.Quotes[itemKey(kind, id)]
	if !ok {
		return nil, &APIError{Status: 404}
	}
	q.Amount *= float64(months) + float64(days)/30 // Quotes hold the one-month price
	q.Amount = math.Round(q.Amount*100) / 100
	return &q, nil
}

func (f *Fake) extension(kind string, id, months, days int, amount float64) Extension {
	e := Extension{Kind: kind, ID: id, Months: months, Days: days, Amount: amount, OK: f.FailExtend == ""}
	if e.OK {
		to := time.Date(2027, 1, 12, 0, 0, 0, 0, time.UTC)
		e.To = &to
	} else {
		e.Error = f.FailExtend
	}
	return e
}

func (f *Fake) Extend(_ context.Context, kind string, id int, in ExtendInput) (*Extension, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.Extends = append(f.Extends, ExtendCall{Kind: kind, ID: id, In: in})
	e := f.extension(kind, id, in.Months, in.Days, in.Amount)
	if !e.OK {
		return nil, &APIError{Status: 400, Message: e.Error}
	}
	return &e, nil
}

func (f *Fake) QuoteTariff(_ context.Context, kind string, id, tariffID int) (*TariffQuote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	q, ok := f.TariffQuotes[itemKey(kind, id)+":"+strconv.Itoa(tariffID)]
	if !ok {
		return nil, &APIError{Status: 400}
	}
	return &q, nil
}

func (f *Fake) ChangeTariff(_ context.Context, kind string, id int, in ChangeTariffInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if f.FailStep != nil {
		return f.FailStep
	}
	f.TariffChanges = append(f.TariffChanges, TariffCall{Kind: kind, ID: id, In: in})
	return nil
}

func (f *Fake) Pay(_ context.Context, customerID int, in PaymentInput) (*PaymentResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	f.Payments = append(f.Payments, in)
	f.nextPaymentID++
	res := &PaymentResult{PaymentID: f.nextPaymentID}
	for _, a := range in.Allocations {
		res.Extensions = append(res.Extensions, f.extension(a.Kind, a.ID, a.Months, a.Days, a.Amount))
	}
	return res, nil
}

func (f *Fake) Provision(_ context.Context, in ProvisionInput) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	if f.FailStep != nil {
		return 0, f.FailStep
	}
	f.Provisions = append(f.Provisions, in)
	return 100 + len(f.Provisions), nil
}

func (f *Fake) ConnectAddon(_ context.Context, subscriptionID int, in ConnectAddonInput) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	if f.FailStep != nil {
		return 0, f.FailStep
	}
	f.AddonCalls = append(f.AddonCalls, AddonCall{SubscriptionID: subscriptionID, In: in})
	return 200 + len(f.AddonCalls), nil
}

func (f *Fake) BaseCurrency(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if f.Currency == "" {
		return "RUB", nil
	}
	return f.Currency, nil
}
