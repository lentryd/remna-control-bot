package telegram

import (
	"strconv"
	"strings"

	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/telegram/view"
)

type route struct {
	kind string // a view.Cb* constant
	id   int    // item id or page number
}

func parseRoute(data string) (route, bool) {
	switch data {
	case view.CbHome, view.CbSubs, view.CbBalance, view.CbRefs, view.CbTariffs:
		return route{kind: data}, true
	}
	// ids start at 1, page numbers at 0
	for p, minID := range map[string]int{view.CbSub: 1, view.CbTariff: 1, view.CbSubsPage: 0, view.CbTariffsPage: 0} {
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

func RefLink(username string, codec *refcode.Codec, customerID int) string {
	return "https://t.me/" + username + "?start=ref_" + codec.Encode(customerID)
}
