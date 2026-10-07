package payments

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// LinkFunc creates a Telegram invoice link in stars (the bot's createInvoiceLink).
type LinkFunc func(ctx context.Context, title, description, payload string, stars int) (string, error)

// Stars is Telegram's own currency: no merchant account and no webhook — the
// bot receives pre_checkout_query and successful_payment updates itself.
type Stars struct {
	RubPerStar float64
	Link       LinkFunc // set once the bot exists
}

var _ Provider = (*Stars)(nil)

func (s *Stars) ID() string    { return "stars" }
func (s *Stars) Title() string { return "Telegram Stars ⭐" }

// maxStars is a sanity cap on one invoice (Telegram applies its own limit too).
const maxStars = 2_500_000

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

// Count is the stars an amount in roubles costs, rounded up (the epsilon
// absorbs float noise such as 100.00000000000001).
func (s *Stars) Count(rub float64) int { return int(math.Ceil(rub/s.RubPerStar - 1e-9)) }

func (s *Stars) Invoice(ctx context.Context, in InvoiceRequest) (Invoice, error) {
	if s.Link == nil {
		return Invoice{}, errors.New("stars: bot is not ready")
	}
	if !finitePositive(s.RubPerStar) {
		return Invoice{}, fmt.Errorf("stars: unusable rate %v", s.RubPerStar)
	}
	if !finitePositive(in.Amount) {
		return Invoice{}, fmt.Errorf("stars: unusable amount %v", in.Amount)
	}
	// Checked as a float: converting an out-of-range float to int is undefined.
	if f := math.Ceil(in.Amount/s.RubPerStar - 1e-9); math.IsNaN(f) || f > maxStars {
		return Invoice{}, fmt.Errorf("stars: %v RUB at %v RUB/star exceeds %d stars", in.Amount, s.RubPerStar, maxStars)
	}
	n := max(s.Count(in.Amount), 1)
	link, err := s.Link(ctx, "Оплата VPN", clip(in.Title, 255), in.OrderID, n)
	if err != nil {
		return Invoice{}, fmt.Errorf("stars: %w", err)
	}
	return Invoice{URL: link, Amount: float64(n), Currency: "XTR"}, nil
}
