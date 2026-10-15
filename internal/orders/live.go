package orders

import (
	"time"

	"remna-control-bot/internal/config"
	"remna-control-bot/internal/payments"
)

// Live is the part of the configuration admins change while the bot runs.
// Read it with Service.Live once per operation so one request sees one
// consistent set of values.
type Live struct {
	PayMode   config.PayMode
	Manual    bool   // payment by transfer with a receipt is offered
	Details   string // where to send money, shown with the amount
	TTL       time.Duration
	Providers *payments.Registry // may be nil: none
}

func (s *Service) Live() Live {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}

func (s *Service) SetLive(l Live) {
	s.mu.Lock()
	s.live = l
	s.mu.Unlock()
}

func (s *Service) UpdateLive(fn func(*Live)) {
	s.mu.Lock()
	fn(&s.live)
	s.mu.Unlock()
}
