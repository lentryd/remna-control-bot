// Package billing is where paying for tariffs/extensions will live. For now
// every call answers ErrNotImplemented and the UI says "coming soon".
package billing

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("billing: not implemented")

// Item is what is being paid for: a tariff to buy or a subscription/add-on
// to extend (Kind: "tariff" | "subscription" | "addon").
type Item struct {
	Kind string
	ID   int
}

type Quote struct {
	Amount float64
}

type Billing interface {
	Quote(ctx context.Context, customerID int, item Item) (Quote, error)
	Pay(ctx context.Context, customerID int, item Item) error
}

type NotImplemented struct{}

func (NotImplemented) Quote(context.Context, int, Item) (Quote, error) { return Quote{}, ErrNotImplemented }
func (NotImplemented) Pay(context.Context, int, Item) error            { return ErrNotImplemented }
