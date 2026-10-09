package payments

import (
	"vpn-control-bot/internal/settings"
)

// BuildInput is everything Build needs besides the settings themselves.
type BuildInput struct {
	Settings  settings.Snapshot
	PublicURL string   // the origin providers call back to (webhooks at PublicURL/pay/<id>)
	WebAppURL string   // where customers land after paying unless a return URL is set
	StarsLink LinkFunc // creates Telegram invoice links; may be nil until the bot exists
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// Build turns a settings snapshot into a registry: providers that are on and
// complete are offered, those that are complete but switched off are retired
// (still resolvable for payments already in flight), the rest do not exist.
func Build(in BuildInput) *Registry {
	s := in.Settings
	back := in.WebAppURL + "/"
	var on, off []Provider
	add := func(enabled, complete bool, p Provider) {
		switch {
		case enabled && complete:
			on = append(on, p)
		case complete:
			off = append(off, p)
		}
	}
	{
		y := s.YooKassa()
		add(y.Enabled, y.Complete(), &YooKassa{ShopID: y.ShopID, Secret: y.SecretKey, ReturnURL: orDefault(y.ReturnURL, back),
			ReceiptEmail: y.ReceiptEmail, VAT: y.VATCode})
	}
	{
		c := s.CryptoBot()
		add(c.Enabled, c.Complete(), &CryptoBot{Token: c.Token, Testnet: c.Testnet, Assets: c.Assets})
	}
	{
		h := s.Heleket()
		add(h.Enabled, h.Complete(), &Heleket{MerchantID: h.MerchantID, APIKey: h.APIKey, ToCurrency: h.ToCurrency,
			Network: h.Network, CallbackURL: in.PublicURL + "/pay/heleket"})
	}
	{
		p := s.Pal24()
		add(p.Enabled, p.Complete(), &Pal24{APIToken: p.APIToken, ShopID: p.ShopID, SignatureToken: p.SignatureToken})
	}
	{
		p := s.Platega()
		add(p.Enabled, p.Complete(), &Platega{MerchantID: p.MerchantID, Secret: p.Secret, PaymentMethod: p.PaymentMethod,
			APIVersion: p.APIVersion, ReturnURL: orDefault(p.ReturnURL, back), FailedURL: p.FailedURL})
	}
	{
		st := s.Stars()
		add(st.Enabled, st.Complete(), &Stars{PerStar: st.Rate, Link: in.StarsLink})
	}
	return NewRegistry(on...).Retire(off...)
}
