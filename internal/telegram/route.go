package telegram

import (
	"strconv"
	"strings"

	"github.com/google/uuid"

	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/telegram/view"
)

type route struct {
	kind string // a view.Cb* constant
	id   int    // item id or page number
}

func parseRoute(data string) (route, bool) {
	switch data {
	case view.CbHome, view.CbSubs, view.CbBalance, view.CbRefs, view.CbTariffs, view.CbOrders:
		return route{kind: data}, true
	}
	// ids start at 1, page numbers at 0
	for p, minID := range map[string]int{view.CbSub: 1, view.CbRenew: 1, view.CbSubsPage: 0, view.CbTariffsPage: 0, view.CbOrdersPage: 0} {
		if rest, ok := strings.CutPrefix(data, p); ok {
			id, err := strconv.Atoi(rest)
			if err != nil || id < minID {
				return route{}, false
			}
			return route{kind: p, id: id}, true
		}
	}
	return route{}, false
}

// parseIDs parses "prefix<a>:<b>:<c>" into n non-negative ints.
func parseIDs(data, prefix string, n int) ([]int, bool) {
	rest, ok := strings.CutPrefix(data, prefix)
	if !ok {
		return nil, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != n {
		return nil, false
	}
	out := make([]int, n)
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// parseOrderID parses "prefix<order uuid>".
func parseOrderID(data, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(data, prefix)
	if _, err := uuid.Parse(rest); !ok || len(rest) != 36 || err != nil {
		return "", false
	}
	return rest, true
}

func RefLink(username string, codec *refcode.Codec, customerID int) string {
	return "https://t.me/" + username + "?start=ref_" + codec.Encode(customerID)
}

// parseProviderCallback parses "pp:<provider>:<order uuid>".
func parseProviderCallback(data string) (provider, orderID string, ok bool) {
	rest, ok := strings.CutPrefix(data, view.CbOrderProvider)
	if !ok {
		return "", "", false
	}
	provider, orderID, ok = strings.Cut(rest, ":")
	if !ok || provider == "" || len(provider) > 20 || strings.ContainsAny(provider, ": ") || len(orderID) != 36 {
		return "", "", false
	}
	if _, err := uuid.Parse(orderID); err != nil {
		return "", "", false
	}
	return provider, orderID, true
}
