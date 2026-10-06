package telegram

import (
	"strconv"
	"strings"

	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/telegram/view"
)

type route struct {
	kind string // a view.Cb* constant
	id   int
}

func parseRoute(data string) (route, bool) {
	switch data {
	case view.CbHome, view.CbSubs, view.CbBalance, view.CbRefs, view.CbTariffs:
		return route{kind: data}, true
	}
	for _, p := range []string{view.CbSub, view.CbTariff} {
		if rest, ok := strings.CutPrefix(data, p); ok {
			id, err := strconv.Atoi(rest)
			if err != nil || id <= 0 {
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
