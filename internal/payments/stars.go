package payments

import (
	"context"
	"errors"
	"fmt"
	"math"

	"vpn-control-bot/internal/i18n"
)

// LinkFunc creates a Telegram invoice link in stars (the bot's createInvoiceLink).
type LinkFunc func(ctx context.Context, title, description, payload string, stars int) (string, error)

// Stars is Telegram's own currency: no merchant account and no webhook — the
// bot receives pre_checkout_query and successful_payment updates itself.
type Stars struct {
	PerStar float64  // base-currency units per star
	Link    LinkFunc // set once the bot exists
}

var _ Provider = (*Stars)(nil)

func (s *Stars) ID() string    { return "stars" }
func (s *Stars) Title() string { return "Telegram Stars ⭐" }

// maxStars is a sanity cap on one invoice (Telegram applies its own limit too).
const maxStars = 2_500_000

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

// Count is the stars an amount in the base currency costs, rounded up (the
// epsilon absorbs float noise such as 100.00000000000001).
func (s *Stars) Count(amount float64) int { return int(math.Ceil(amount/s.PerStar - 1e-9)) }

func (s *Stars) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	if s.Link == nil {
		return Invoice{}, errors.New("stars: bot is not ready")
	}
	if !finitePositive(s.PerStar) {
		return Invoice{}, fmt.Errorf("stars: unusable rate %v", s.PerStar)
	}
	if !finitePositive(in.Amount) {
		return Invoice{}, fmt.Errorf("stars: unusable amount %v", in.Amount)
	}
	// Checked as a float: converting an out-of-range float to int is undefined.
	if f := math.Ceil(in.Amount/s.PerStar - 1e-9); math.IsNaN(f) || f > maxStars {
		return Invoice{}, fmt.Errorf("stars: %v %s at %v %s/star exceeds %d stars", in.Amount, in.Currency, s.PerStar, in.Currency, maxStars)
	}
	n := max(s.Count(in.Amount), 1)
	// The payer's language when the request carries it (the bot's own flow), else English.
	link, err := s.Link(ctx, i18n.FromContext(ctx).T("invoice.title"), clip(in.Title, 255), in.OrderID, n)
	if err != nil {
		return Invoice{}, fmt.Errorf("stars: %w", err)
	}
	return Invoice{URL: link, Amount: float64(n), Currency: "XTR"}, nil
}
