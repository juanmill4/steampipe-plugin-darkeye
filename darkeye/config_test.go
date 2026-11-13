package darkeye

import (
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/turbot/steampipe-plugin-sdk/v6/plugin"
)

func decode(t *testing.T, content string) Config {
	t.Helper()
	f, d := hclsyntax.ParseConfig([]byte(content), "test.spc", hcl.InitialPos)
	if d.HasErrors() {
		t.Fatal(d.Error())
	}
	var c Config
	if d := gohcl.DecodeBody(f.Body, nil, &c); d.HasErrors() {
		t.Fatal(d.Error())
	}
	return c
}

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestResolveOptions(t *testing.T) {
	env := envMap(map[string]string{"HIBR_API_KEY": "env-hibr", "BREACHHOUSE_API_KEY": "env-bh"})

	o, err := resolveOptions(decode(t, ""), env)
	if err != nil || o.HIBRKey != "env-hibr" || o.BHKey != "env-bh" || o.MaxPages != 0 {
		t.Fatalf("env fallback / no cap by default: %+v %v", o, err)
	}
	o, err = resolveOptions(decode(t, "haveibeenransom_api_key = \"cfg\"\nmax_pages = 3"), env)
	if err != nil || o.HIBRKey != "cfg" || o.BHKey != "env-bh" || o.MaxPages != 3 {
		t.Fatalf("config overrides env: %+v %v", o, err)
	}
	o, err = resolveOptions(decode(t, "breachhouse_api_key = \"\""), env)
	if err != nil || o.BHKey != "" || o.HIBRKey != "env-hibr" {
		t.Fatalf("empty value disables a product: %+v %v", o, err)
	}
	// Keys are optional: a connection with none still loads; each table reports its own missing key.
	if _, err = resolveOptions(Config{}, envMap(nil)); err != nil {
		t.Fatalf("no keys must not fail validation: %v", err)
	}
	for _, v := range []string{"0", "-1"} {
		_, err = resolveOptions(decode(t, "max_pages = "+v), env)
		if err == nil || !strings.Contains(err.Error(), "max_pages") {
			t.Fatalf("max_pages=%s accepted: %v", v, err)
		}
	}
}

func TestGetConfigAcceptsValueAndPointer(t *testing.T) {
	k := "k"
	conn := &plugin.Connection{Name: "darkeye"}
	if c := GetConfig(conn); c.HIBRKey != nil {
		t.Fatal("empty connection")
	}
	if c := GetConfig(nil); c.HIBRKey != nil {
		t.Fatal("nil connection")
	}
	conn.SetConfig(Config{HIBRKey: &k})
	if c := GetConfig(conn); c.HIBRKey == nil || *c.HIBRKey != "k" {
		t.Fatal("value config")
	}
	conn.SetConfig(&Config{BHKey: &k})
	if c := GetConfig(conn); c.BHKey == nil || *c.BHKey != "k" {
		t.Fatal("pointer config")
	}
}

func TestVersionAndUserAgent(t *testing.T) {
	if !strings.HasPrefix(Version, "0.2.0") || userAgent != "steampipe-plugin-darkeye/"+Version {
		t.Fatalf("version %q ua %q", Version, userAgent)
	}
}
