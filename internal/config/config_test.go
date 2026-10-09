package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{"BOT_TOKEN": "t", "VPNCTL_URL": "http://p/", "VPNCTL_TOKEN": "x", "PUBLIC_URL": "https://b.example/", "REF_SECRET": "s", "ADMIN_IDS": "5"}
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.VpnctlURL != "http://p" || c.PublicURL != "https://b.example" || c.WebAppPath != "/bot" || c.HTTPAddr != ":8090" ||
		c.DBPath != "/data/bot.db" || c.LogLevel != "info" || len(c.AdminIDs) != 1 {
		t.Fatalf("defaults: %+v", c)
	}
	if c.WebAppURL() != "https://b.example/bot" {
		t.Fatalf("webapp url %q", c.WebAppURL())
	}
}

func TestLoadRequired(t *testing.T) {
	for _, k := range []string{"BOT_TOKEN", "VPNCTL_URL", "VPNCTL_TOKEN", "PUBLIC_URL", "REF_SECRET", "ADMIN_IDS"} {
		m := base()
		delete(m, k)
		if _, err := load(env(m)); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s: %v", k, err)
		}
	}
}

func TestWebAppPath(t *testing.T) {
	for in, want := range map[string]string{"/": "", "x": "/x", "/x/y/": "/x/y"} {
		m := base()
		m["WEBAPP_PATH"] = in
		c, err := load(env(m))
		if err != nil || c.WebAppPath != want {
			t.Errorf("%q → %q %v, want %q", in, c.WebAppPath, err, want)
		}
	}
}

func TestAdmins(t *testing.T) {
	m := base()
	m["ADMIN_IDS"] = " 5, 7 ,"
	c, err := load(env(m))
	if err != nil || len(c.AdminIDs) != 2 || !c.IsAdmin(7) || c.IsAdmin(9) {
		t.Fatalf("%+v %v", c, err)
	}
	for _, v := range []string{"", " , "} {
		m := base()
		m["ADMIN_IDS"] = v
		if _, err := load(env(m)); err == nil || !strings.Contains(err.Error(), "ADMIN_IDS") {
			t.Errorf("ADMIN_IDS=%q: %v", v, err)
		}
	}
	for _, k := range []string{"ADMIN_IDS"} {
		m := base()
		m[k] = "x"
		if _, err := load(env(m)); err == nil {
			t.Errorf("%s=x accepted", k)
		}
	}
}
