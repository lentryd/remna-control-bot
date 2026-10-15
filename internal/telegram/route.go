package telegram

import (
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
	if data == view.CbHome {
		return route{kind: data}, true
	}
	return route{}, false
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
