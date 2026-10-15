package remnactl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func server(t *testing.T, h http.HandlerFunc) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", "tok")
}

func TestLookupByTelegram(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/customers/lookup" || r.URL.Query().Get("telegram_id") != "77" {
			t.Errorf("unexpected %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"id":5,"name":"Ann","telegram_id":77,"balance":120.5,"subscriptions":[{"id":1,"title":"Main","rw_user":{"short_uuid":"abc","subscription_url":"https://s/abc"},"addons":[]}]}`))
	})
	cu, err := c.LookupByTelegram(context.Background(), 77)
	if err != nil {
		t.Fatal(err)
	}
	if cu.ID != 5 || cu.Balance != 120.5 || cu.Subscriptions[0].RwUser.SubscriptionURL != "https://s/abc" {
		t.Fatalf("bad decode: %+v", cu)
	}
}

func TestErrors(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("short_uuid") {
		case "missing":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"customer not found","code":"customer.not_found"}`))
		default:
			w.WriteHeader(502)
		}
	})
	_, err := c.LookupByShortUUID(context.Background(), "missing")
	var ae *APIError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &ae) || ae.Code != "customer.not_found" {
		t.Fatalf("want not found APIError, got %v", err)
	}
	if _, err := c.LookupByShortUUID(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
	if _, err := New("http://127.0.0.1:1", "tok").GetCustomer(context.Background(), 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("transport error: want unavailable, got %v", err)
	}
}

func TestCreateAndUpdate(t *testing.T) {
	var got CustomerInput
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/customers":
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":9}`))
		case r.Method == "PUT" && r.URL.Path == "/api/v1/customers/9":
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	tg, ref := int64(5), 3
	id, err := c.CreateCustomer(context.Background(), CustomerInput{Name: "N", TelegramID: &tg, ReferrerID: &ref})
	if err != nil || id != 9 || *got.ReferrerID != 3 {
		t.Fatalf("create: id=%d err=%v body=%+v", id, err, got)
	}
	if err := c.UpdateCustomer(context.Background(), 9, CustomerInput{Name: "M", Notes: "keep"}); err != nil || got.Notes != "keep" {
		t.Fatalf("update: err=%v body=%+v", err, got)
	}
}

func TestListsForNotifications(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/customers":
			_, _ = w.Write([]byte(`[{"id":5,"telegram_id":77,"archived":false},{"id":6}]`))
		case "/api/v1/subscriptions":
			_, _ = w.Write([]byte(`[{"id":1,"customer_id":5,"tariff_id":2,"title":"Main","rw_user":{"used_traffic_bytes":9,"traffic_limit_bytes":10,"traffic_limit_strategy":"MONTH","next_traffic_reset_at":"2026-11-01T00:00:00Z"},"addons":[{"id":3,"addon_id":4,"tariff_id":7,"addon_name":"lte"}]}]`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	cs, err := c.ListCustomers(context.Background())
	if err != nil || len(cs) != 2 || *cs[0].TelegramID != 77 || cs[1].TelegramID != nil {
		t.Fatalf("customers: %+v %v", cs, err)
	}
	subs, err := c.ListSubscriptions(context.Background())
	if err != nil || len(subs) != 1 {
		t.Fatalf("subscriptions: %v", err)
	}
	s := subs[0]
	if s.CustomerID != 5 || *s.TariffID != 2 || s.RwUser.NextTrafficResetAt == nil || s.RwUser.TrafficLimitStrategy != "MONTH" ||
		s.Addons[0].AddonID != 4 || *s.Addons[0].TariffID != 7 {
		t.Fatalf("subscription decode: %+v", s)
	}
}
