package vpnctl

import "time"

// Customer mirrors vpn-control's CustomerDetail (only the fields the bot uses).
type Customer struct {
	ID              int        `json:"id"`
	Name            string     `json:"name"`
	Contact         string     `json:"contact"`
	TelegramID      *int64     `json:"telegram_id"`
	Notes           string     `json:"notes"`
	ReferrerID      *int       `json:"referrer_id"`
	ReferralPercent *float64   `json:"referral_percent"`
	Archived        bool       `json:"archived"`
	Balance         float64    `json:"balance"`
	BalanceOwn      float64    `json:"balance_own"`
	BalanceReferral float64    `json:"balance_referral"`
	Monthly         float64    `json:"monthly"`
	NearestExpireAt *time.Time `json:"nearest_expire_at"`
	TotalPaid       float64    `json:"total_paid"`

	Subscriptions []Subscription `json:"subscriptions"`
	Ledger        []LedgerEntry  `json:"ledger"`
	Referrals     []Referee      `json:"referrals"`
	Accruals      []Accrual      `json:"accruals"`
}

type Referee struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Archived  bool    `json:"archived"`
	Monthly   float64 `json:"monthly"`
	TotalPaid float64 `json:"total_paid"`
}

type Accrual struct {
	ReferrerID int     `json:"referrer_id"`
	RefereeID  int     `json:"referee_id"`
	Amount     float64 `json:"amount"`
	Status     string  `json:"status"`
}

type LedgerEntry struct {
	ID     int       `json:"id"`
	Type   string    `json:"type"` // payment|charge|adjustment|refund|referral
	Amount float64   `json:"amount"`
	Date   time.Time `json:"date"`
	Note   string    `json:"note"`
}

type Subscription struct {
	ID         int     `json:"id"`
	CustomerID int     `json:"customer_id"`
	TariffID   *int    `json:"tariff_id"`
	Title      string  `json:"title"`
	Label      string  `json:"label"`
	TariffName string  `json:"tariff_name"`
	Price      float64 `json:"price"`
	RwUser     *RwUser `json:"rw_user"`
	Addons     []Addon `json:"addons"`
}

type Addon struct {
	ID         int     `json:"id"`
	AddonID    int     `json:"addon_id"`
	TariffID   *int    `json:"tariff_id"`
	AddonName  string  `json:"addon_name"`
	TariffName string  `json:"tariff_name"`
	Price      float64 `json:"price"`
	Included   bool    `json:"included"`
	RwUser     *RwUser `json:"rw_user"`
}

type RwUser struct {
	ShortUUID         string     `json:"short_uuid"`
	Status            string     `json:"status"`
	ExpireAt          *time.Time `json:"expire_at"`
	Unlimited         bool       `json:"unlimited"`
	UsedTrafficBytes  int64      `json:"used_traffic_bytes"`
	TrafficLimitBytes int64      `json:"traffic_limit_bytes"`
	// TrafficLimitStrategy is NO_RESET, DAY, WEEK, MONTH or MONTH_ROLLING.
	TrafficLimitStrategy string     `json:"traffic_limit_strategy"`
	NextTrafficResetAt   *time.Time `json:"next_traffic_reset_at"`
	HwidDeviceLimit      *int       `json:"hwid_device_limit"`
	SubscriptionURL      string     `json:"subscription_url"`
	Deleted              bool       `json:"deleted"`
}

type Tariff struct {
	ID                int      `json:"id"`
	Kind              string   `json:"kind"` // base|addon
	AddonID           *int     `json:"addon_id"`
	AddonName         string   `json:"addon_name"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	MonthlyPrice      float64  `json:"monthly_price"`
	Active            bool     `json:"active"`
	SortOrder         int      `json:"sort_order"`
	TrafficLimitBytes int64    `json:"traffic_limit_bytes"`
	HwidLimit         *int     `json:"hwid_limit"`
	Periods           []Period `json:"periods"`
}

type Period struct {
	Months int     `json:"months"`
	Days   int     `json:"days"`
	Price  float64 `json:"price"`
}

// CustomerInput is vpn-control's customerInput. PUT replaces every field.
type CustomerInput struct {
	Name            string   `json:"name"`
	Contact         string   `json:"contact"`
	TelegramID      *int64   `json:"telegram_id"`
	Notes           string   `json:"notes"`
	ReferrerID      *int     `json:"referrer_id"`
	ReferralPercent *float64 `json:"referral_percent"`
	Archived        bool     `json:"archived"`
}

// Input copies every writable field, so a PUT built from it changes only
// what the caller modifies afterwards.
func (c *Customer) Input() CustomerInput {
	return CustomerInput{
		Name: c.Name, Contact: c.Contact, TelegramID: c.TelegramID, Notes: c.Notes,
		ReferrerID: c.ReferrerID, ReferralPercent: c.ReferralPercent, Archived: c.Archived,
	}
}
