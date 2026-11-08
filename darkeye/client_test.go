package darkeye

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

const emptyBH = `{"ransomware":[],"breaches":[],"leads":[],"stealers":[]}`

func testClient(t *testing.T, handler http.HandlerFunc) *client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c := newClient()
	c.hibr = s.URL + "/hibr/"
	c.bh = s.URL + "/bh"
	return c
}

func opts() Options {
	return Options{HIBRKey: "synthetic-hibr", BHKey: "synthetic-bh", MaxPages: 5, Limit: 25}
}

func TestPaginationAndMetadata(t *testing.T) {
	calls := []string{}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Method != "GET" || r.Header.Get("Accept") != "application/json" {
			t.Error("unexpected request")
		}
		if r.URL.Path == "/bh" {
			if r.Header.Get("Authorization") != "Bearer synthetic-bh" || r.URL.Query().Get("q") != "example.com" || r.URL.Query().Get("limit") != "25" {
				t.Error("wrong BH request")
			}
			fmt.Fprint(w, `{"ransomware":[],"breaches":{"total":2,"results":[{"id":1,"website":"https://sub.example.com","name":"Indexed listing","password":"DO-NOT-RETURN","download_url":"https://blocked.invalid"},{"id":2,"website":"notexample.com"}]},"leads":[],"stealers":[]}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-hibr" {
			t.Error("wrong HIBR key")
		}
		switch r.URL.Path {
		case "/hibr/example.com":
			fmt.Fprint(w, `{"sources":[{"id_source":1,"post_title":"Listing","leak_type":"ransomware_leak","leaked_data":"DO-NOT-RETURN"}],"has_next":true}`)
		case "/hibr/example.com/2":
			fmt.Fprint(w, `{"sources":[],"has_next":true}`)
		case "/hibr/example.com/3":
			fmt.Fprint(w, `{"results":[{"id_source":1,"post_title":"Listing","leak_type":"ransomware_leak"},{"id_source":2,"leak_type":"traditional_breach"}],"pagination":{"has_next":false}}`)
		default:
			t.Error("unexpected path")
			w.WriteHeader(500)
		}
	})
	r, err := c.lookup(context.Background(), "EXAMPLE.com.", opts())
	if err != nil || len(calls) != 4 || len(r.Records) != 3 || !r.Complete || r.Truncated || len(r.Products) != 2 {
		t.Fatalf("report=%+v error=%v calls=%v", r, err, calls)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "DO-NOT-RETURN") || strings.Contains(string(b), "blocked.invalid") {
		t.Fatal("returned unapproved fields")
	}
	if r.Records[0].Category != "ransomware" || r.Records[2].Product != bhProduct {
		t.Fatal("lost provenance")
	}
}

func TestFailureIsNotEmptySuccess(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/bh" {
					fmt.Fprint(w, emptyBH)
					return
				}
				w.WriteHeader(status)
				fmt.Fprint(w, "synthetic-hibr RESPONSE-SECRET")
			})
			r, err := c.lookup(context.Background(), "example.com", opts())
			if err != nil || r.Complete || len(r.Errors) != 1 || len(r.Products) != 2 {
				t.Fatalf("%+v %v", r, err)
			}
			b, _ := json.Marshal(r)
			if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "synthetic-hibr") {
				t.Fatal("secret in error")
			}
		})
	}
	for _, body := range []string{`{`, `[]`, `null`, `{}`, `{"success":false}`, `{"error":"SECRET"}`, `{"sources":[],"has_next":"false"}`, `{"sources":[null],"has_next":false}`} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/bh" {
					fmt.Fprint(w, emptyBH)
				} else {
					fmt.Fprint(w, body)
				}
			})
			r, err := c.lookup(context.Background(), "example.com", opts())
			if err != nil || r.Complete || len(r.Errors) != 1 {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	r, err := c.lookup(context.Background(), "example.com", opts())
	if err != nil || r.Complete || len(r.Errors) != 2 || len(r.Records) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestKeysAndInvalidInputs(t *testing.T) {
	for _, product := range []string{"hibr", "bh"} {
		t.Run(product, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if product == "hibr" {
					if r.URL.Path == "/bh" {
						t.Fatal("unconfigured BH called")
					}
					fmt.Fprint(w, `{"sources":[],"has_next":false}`)
				} else {
					if r.URL.Path != "/bh" {
						t.Fatal("unconfigured HIBR called")
					}
					fmt.Fprint(w, emptyBH)
				}
			})
			o := opts()
			if product == "hibr" {
				o.BHKey = ""
			} else {
				o.HIBRKey = ""
			}
			r, err := c.lookup(context.Background(), "example.com", o)
			if err != nil || calls != 1 || !r.Complete {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input made network call") })
	for _, domain := range []string{"*.example.com", "example.com?x", "https://example.com", "user@example.com", "127.0.0.1", "-bad.com", "a..com", "example.com..", "example_com.com"} {
		if _, err := c.lookup(context.Background(), domain, opts()); err == nil {
			t.Errorf("accepted %q", domain)
		}
	}
	for _, o := range []Options{{MaxPages: 5, Limit: 25}, {HIBRKey: "key", MaxPages: 0, Limit: 25}, {HIBRKey: "key", MaxPages: 51, Limit: 25}, {BHKey: "key", MaxPages: 5, Limit: 201}} {
		if _, err := c.lookup(context.Background(), "example.com", o); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	if d, err := normalizeDomain("bücher.example"); err != nil || d != "xn--bcher-kva.example" {
		t.Fatalf("IDNA: %s %v", d, err)
	}
}

func TestPartialPagesAndLimits(t *testing.T) {
	for _, failSecond := range []bool{true, false} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/bh" {
				fmt.Fprint(w, emptyBH)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/2") && failSecond {
				w.WriteHeader(500)
				return
			}
			fmt.Fprint(w, `{"sources":[{"id_source":1}],"has_next":true}`)
		})
		o := opts()
		o.MaxPages = 2
		r, err := c.lookup(context.Background(), "example.com", o)
		if err != nil || r.Complete || len(r.Records) != 1 || (failSecond && len(r.Errors) != 1) || (!failSecond && !r.Truncated) {
			t.Fatalf("%+v %v", r, err)
		}
	}
	for _, body := range []string{`{"ransomware":{"results":[],"total":1},"breaches":[],"leads":[],"stealers":[]}`, `{"ransomware":[{"id":1,"website":"example.com"}],"breaches":[],"leads":[],"stealers":[]}`} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		o := opts()
		o.HIBRKey = ""
		o.Limit = 1
		r, err := c.lookup(context.Background(), "example.com", o)
		if err != nil || !r.Truncated || r.Complete {
			t.Fatalf("%+v %v", r, err)
		}
	}
}

func TestBHValidationAndBoundaries(t *testing.T) {
	for _, site := range []string{"example.com.evil.test", "notexample.com", "https://example.com@evil.test", "javascript://example.com", "https://evil.test/example.com"} {
		if matchesDomain(Record{Website: site, Title: "example.com"}, "example.com") {
			t.Errorf("accepted unrelated %s", site)
		}
	}
	for _, title := range []string{"notexample.com", "example.com.evil.test", "_example.com", "bad-example.com"} {
		if matchesDomain(Record{Title: title}, "example.com") {
			t.Errorf("accepted unrelated title %s", title)
		}
	}
	for _, title := range []string{"Listing for example.com", "(sub.example.com)", "example.com."} {
		if !matchesDomain(Record{Title: title}, "example.com") {
			t.Errorf("missed %s", title)
		}
	}
	for _, body := range []string{`{}`, `{"ransomware":[]}`, `{"ransomware":null,"breaches":[],"leads":[],"stealers":[]}`, `{"ransomware":{"results":[{}],"total":0},"breaches":[],"leads":[],"stealers":[]}`} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		o := opts()
		o.HIBRKey = ""
		r, err := c.lookup(context.Background(), "example.com", o)
		if err != nil || r.Complete || len(r.Errors) != 1 {
			t.Fatalf("%+v %v", r, err)
		}
	}
}

func TestRedirectSizeAndCancellation(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	o := opts()
	o.BHKey = ""
	r, err := c.lookup(context.Background(), "example.com", o)
	if err != nil || calls != 0 || len(r.Errors) != 1 || r.Errors[0].Message != "HTTP 302" {
		t.Fatalf("%+v %v", r, err)
	}
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat(" ", maxBody+1)) })
	r, err = c.lookup(context.Background(), "example.com", o)
	if err != nil || len(r.Errors) != 1 || !strings.Contains(r.Errors[0].Message, "size limit") {
		t.Fatalf("%+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.lookup(ctx, "example.com", o); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
}

func TestOptionalConfigAndSchema(t *testing.T) {
	t.Setenv("HIBR_API_KEY", "env-hibr")
	t.Setenv("BREACHHOUSE_API_KEY", "")
	for _, content := range []string{"", `haveibeenransom_api_key = "explicit"`, `breachhouse_api_key = "bh"
max_pages = 2
limit = 10`} {
		f, d := hclsyntax.ParseConfig([]byte(content), "test.spc", hcl.InitialPos)
		if d.HasErrors() {
			t.Fatal(d.Error())
		}
		var c Config
		if d := gohcl.DecodeBody(f.Body, nil, &c); d.HasErrors() {
			t.Fatal(d.Error())
		}
		if err := c.options().validate(); err != nil {
			t.Fatal(err)
		}
	}
	p := Plugin(context.Background())
	table := p.TableMap["darkeye_domain"]
	if len(p.TableMap) != 1 || table.List.KeyColumns[0].Name != "domain" || len(table.Columns) != 7 {
		t.Fatal("unexpected schema")
	}
}
