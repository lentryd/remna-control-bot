package vpnctl

import (
	"context"
	"sort"
	"sync"
)

// Fake is an in-memory Client that behaves like vpn-control's lookup rules.
type Fake struct {
	mu        sync.Mutex
	customers map[int]*Customer
	nextID    int
	Tariffs   []Tariff
	Creates   []CustomerInput
	Updates   []Update
	Err       error // returned by every call when set
}

type Update struct {
	ID int
	In CustomerInput
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
