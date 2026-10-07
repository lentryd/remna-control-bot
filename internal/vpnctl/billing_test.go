package vpnctl

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestPay(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/customers/5/payments" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		var in PaymentInput
		json.NewDecoder(r.Body).Decode(&in)
		if in.Amount != 700 || in.Method != "manual" || len(in.Allocations) != 1 || in.Allocations[0].Amount != 900 {
			t.Errorf("body %+v", in)
		}
		w.Write([]byte(`{"payment_id":42,"balance":0,"extensions":[{"kind":"subscription","id":3,"ok":true,"to":"2027-01-12T00:00:00Z"}]}`))
	})
	res, err := c.Pay(context.Background(), 5, PaymentInput{Amount: 700, Method: "manual", Note: "bot order #1",
		Allocations: []Allocation{{Kind: "subscription", ID: 3, Months: 3, Amount: 900}}})
	if err != nil || res.PaymentID != 42 || !res.Extensions[0].OK || res.Extensions[0].To.Year() != 2027 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestQuoteExtendAndProvision(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/items/addon/8/quote":
			if r.URL.Query().Get("months") != "3" || r.URL.Query().Get("days") != "0" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"customer_id":5,"amount":450.5,"from":"2026-10-07T00:00:00Z","to":"2027-01-07T00:00:00Z","monthly":150,"periods":[],"balance":10}`))
		case "/api/v1/subscriptions/provision":
			w.WriteHeader(201)
			w.Write([]byte(`{"id":77}`))
		case "/api/v1/subscriptions/3/addons":
			w.WriteHeader(201)
			w.Write([]byte(`{"id":78}`))
		case "/api/v1/items/subscription/3/tariff-quote":
			w.Write([]byte(`{"old_monthly":100,"new_monthly":200,"surcharge":66.67}`))
		case "/api/v1/items/subscription/3/tariff":
			w.WriteHeader(204)
		case "/api/v1/items/subscription/3/extend":
			w.Write([]byte(`{"kind":"subscription","id":3,"ok":true}`))
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	ctx := context.Background()
	q, err := c.QuoteExtend(ctx, "addon", 8, 3, 0)
	if err != nil || q.Amount != 450.5 || q.CustomerID != 5 {
		t.Fatalf("quote %+v %v", q, err)
	}
	if id, err := c.Provision(ctx, ProvisionInput{CustomerID: 5, TariffID: 2, Username: "bot_c5_o1", Months: 1, Amount: 100}); err != nil || id != 77 {
		t.Fatalf("provision %d %v", id, err)
	}
	if id, err := c.ConnectAddon(ctx, 3, ConnectAddonInput{TariffID: 9, Months: 1, Amount: 50}); err != nil || id != 78 {
		t.Fatalf("addon %d %v", id, err)
	}
	if tq, err := c.QuoteTariff(ctx, "subscription", 3, 9); err != nil || tq.Surcharge != 66.67 {
		t.Fatalf("tariff quote %+v %v", tq, err)
	}
	if err := c.ChangeTariff(ctx, "subscription", 3, ChangeTariffInput{TariffID: 9, Surcharge: 66.67}); err != nil {
		t.Fatal(err)
	}
	if e, err := c.Extend(ctx, "subscription", 3, ExtendInput{Months: 1, Amount: 100}); err != nil || !e.OK {
		t.Fatalf("extend %+v %v", e, err)
	}
}
