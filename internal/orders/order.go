package orders

import (
	"context"
	"errors"
	"slices"
	"time"

	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/store"
)

type Kind string

const (
	KindRenew        Kind = "renew"
	KindSubscribe    Kind = "subscribe"
	KindAddon        Kind = "addon"
	KindChangeTariff Kind = "change_tariff"
	KindTopup        Kind = "topup"
)

type Status string

const (
	StatusNew             Status = "new"
	StatusAwaitingReceipt Status = "awaiting_receipt"
	StatusAwaitingPayment Status = "awaiting_payment" // an invoice is issued, the provider will confirm
	StatusPendingReview   Status = "pending_review"
	StatusApproving       Status = "approving"
	StatusApproved        Status = "approved"
	StatusCredited        Status = "credited"
	StatusFailed          Status = "failed"
	StatusRejected        Status = "rejected"
	StatusExpired         Status = "expired"
	StatusCancelled       Status = "cancelled"
)

var allStatuses = []Status{StatusNew, StatusAwaitingReceipt, StatusAwaitingPayment, StatusPendingReview, StatusApproving,
	StatusApproved, StatusCredited, StatusFailed, StatusRejected, StatusExpired, StatusCancelled}

// Valid: s is one of the order statuses (admin filters come from the client).
func (s Status) Valid() bool { return slices.Contains(allStatuses, s) }

type Method string

const (
	MethodManual  Method = "manual"
	MethodBalance Method = "balance"
)

// IsProvider: the method is an online payment provider's id ("yookassa", …).
func (m Method) IsProvider() bool { return m != "" && m != MethodManual && m != MethodBalance }

type ReceiptKind string

const (
	ReceiptPhoto    ReceiptKind = "photo"
	ReceiptDocument ReceiptKind = "document"
	ReceiptText     ReceiptKind = "text"
)

type Request struct {
	Kind     Kind    `json:"kind"`
	ItemKind string  `json:"item_kind"` // renew, change_tariff: subscription | addon
	ItemID   int     `json:"item_id"`   // renew, change_tariff: the item; addon: the subscription
	TariffID int     `json:"tariff_id"` // subscribe, addon, change_tariff
	Months   int     `json:"months"`
	Days     int     `json:"days"`
	Amount   float64 `json:"amount"` // topup only
}

type Quote struct {
	Title       string            `json:"title"` // a stored i18n.Msg, like Order.Title
	Price       float64           `json:"price"`
	FromBalance float64           `json:"from_balance"`
	ToPay       float64           `json:"to_pay"`
	Methods     []Method          `json:"methods"`
	Titles      map[string]string `json:"titles,omitempty"` // provider id → button label
}

type Order struct {
	ID             string
	CustomerID     int
	ChatID         int64
	Request               // embedded: Kind, ItemKind, ItemID, TariffID, Months, Days, Amount (Amount unused after creation)
	Title          string // a stored i18n.Msg (read it with Localizer.Text); plain text in rows from before localization
	Price          float64
	FromBalance    float64
	ToPay          float64
	Received       *float64
	Method         Method
	Status         Status
	ReceiptKind    ReceiptKind
	ReceiptFileID  string
	Comment        string
	RejectReason   string
	Admin          string
	Decision       Decision
	PanelPaymentID int
	ResultUntil    *time.Time
	Error          string
	ExternalID     string  // provider's payment/invoice id
	PayURL         string  // where the customer pays
	PayAmount      float64 // what was invoiced, in PayCurrency (the base currency, or stars for Stars)
	PayCurrency    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Final statuses never change again.
func (s Status) Final() bool {
	switch s {
	case StatusApproved, StatusCredited, StatusRejected, StatusExpired, StatusCancelled:
		return true
	}
	return false
}

func (o Order) Paid() float64 {
	if o.Received != nil {
		return *o.Received
	}
	return o.ToPay
}

// Note links the panel payment back to the order.
func (o Order) Note() string { return "bot order #" + o.ID }

// Short is the order number people see and quote: the start of the UUID.
func (o Order) Short() string { return ShortID(o.ID) }

func ShortID(id string) string { return id[:min(len(id), 8)] }

type Notifier interface {
	PayScreen(ctx context.Context, o Order, details string) error // customer chat: amount + details + buttons
	// Card tells the admins where the order stands: a new card when it needs a decision, an edit of the cards already
	// sent otherwise. Failures are per admin and never fail the order.
	Card(ctx context.Context, o Order) error
	Customer(ctx context.Context, o Order) error // decision notice to the customer
}

// Alert is a catalog message for the admins (Key "alert.…" with its Args), rendered in each admin's language.
type Alert = i18n.Msg

// Alerter is an optional Notifier capability: a plain message to every admin.
type Alerter interface {
	Alert(ctx context.Context, a Alert) error
}

type ChatStates interface { // implemented by *store.Store
	SetChatState(ctx context.Context, chatID int64, st store.ChatState) error
	GetChatState(ctx context.Context, chatID int64) (store.ChatState, bool, error)
	ClearChatState(ctx context.Context, chatID int64) error
}

var (
	ErrStateChanged = errors.New("orders: state changed")
	ErrNotFound     = errors.New("orders: not found")
	ErrBadRequest   = errors.New("orders: bad request")
	ErrManualOff    = errors.New("orders: manual payment is off")
	ErrUnderpaid    = errors.New("orders: received less than to pay")
	ErrProvider     = errors.New("orders: payment provider failed")
)
