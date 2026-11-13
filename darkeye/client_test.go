package darkeye

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const emptyBH = `{"ransomware":[],"breaches":[],"leads":[],"stealers":[]}`

func testClient(t *testing.T, o Options, handler http.HandlerFunc) *client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c := newClient(o)
	c.hibr = s.URL + "/hibr/"
	c.bh = s.URL + "/bh"
	return c
}

func collect(into *[]Record, keepGoing func(n int) bool) func(Record) bool {
	return func(r Record) bool {
		*into = append(*into, r)
		return keepGoing == nil || keepGoing(len(*into))
	}
}

func hibrPages(t *testing.T, calls *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.URL.Path)
		if r.Method != "GET" || r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != userAgent {
			t.Error("unexpected request headers")
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-hibr" {
			t.Error("wrong HIBR key")
		}
		switch r.URL.Path {
		case "/hibr/example.com":
			fmt.Fprint(w, `{"sources":[{"id_source":1,"post_title":"Listing","leak_type":"ransomware_leak","group_name":"grp","country":"US","website":"example.com","discovered":"2024-12-16 21:07:57.040833","leaked_data":"DO-NOT-RETURN"}],"has_next":true}`)
		case "/hibr/example.com/2":
			fmt.Fprint(w, `{"sources":[],"has_next":true}`)
		case "/hibr/example.com/3":
			fmt.Fprint(w, `{"results":[{"id_source":1,"post_title":"Listing","leak_type":"ransomware_leak"},{"id_source":2,"leak_type":"traditional_breach","discovered":"2025-01-02"},{"id_source":3}],"pagination":{"current_page":3,"has_next":true}}`)
		case "/hibr/example.com/4":
			fmt.Fprint(w, `{"results":[{"id_source":4,"post_title":"Last"}],"pagination":{"has_next":false}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}
}

func TestHIBRPaginatesUntilNoMoreResults(t *testing.T) {
	var calls []string
	c := testClient(t, Options{HIBRKey: "synthetic-hibr"}, hibrPages(t, &calls))
	var got []Record
	capped, err := c.streamHIBR(context.Background(), "example.com", collect(&got, nil))
	if err != nil || capped {
		t.Fatalf("capped=%v err=%v", capped, err)
	}
	if len(calls) != 4 || len(got) != 4 {
		t.Fatalf("calls=%v records=%d", calls, len(got))
	}
	first := got[0]
	if first.Product != hibrProduct || first.Category != "ransomware" || first.ID != "1" || first.Title != "Listing" ||
		first.Website != "example.com" || first.Group != "grp" || first.Country != "US" {
		t.Fatalf("mapping: %+v", first)
	}
	want := time.Date(2024, 12, 16, 21, 7, 57, 40833000, time.UTC)
	if first.Discovered == nil || !first.Discovered.Equal(want) {
		t.Fatalf("discovered: %v", first.Discovered)
	}
	if got[1].Category != "breaches" || got[1].Discovered == nil || got[1].Discovered.Format("2006-01-02") != "2025-01-02" {
		t.Fatalf("breach mapping: %+v", got[1])
	}
	if got[2].Category != "leaks" || got[2].Discovered != nil {
		t.Fatalf("unknown type must be leaks with NULL discovered: %+v", got[2])
	}
}

func TestHIBRStopsWhenNoRowsRemain(t *testing.T) {
	var calls []string
	c := testClient(t, Options{HIBRKey: "synthetic-hibr"}, hibrPages(t, &calls))
	var got []Record
	// Equivalent of RowsRemaining() == 0 after the first row (SQL "limit 1").
	capped, err := c.streamHIBR(context.Background(), "example.com", collect(&got, func(n int) bool { return n < 1 }))
	if err != nil || capped || len(got) != 1 || len(calls) != 1 {
		t.Fatalf("capped=%v err=%v records=%d calls=%v", capped, err, len(got), calls)
	}
}

func TestHIBRMaxPagesCap(t *testing.T) {
	var calls []string
	c := testClient(t, Options{HIBRKey: "synthetic-hibr", MaxPages: 2}, hibrPages(t, &calls))
	var got []Record
	capped, err := c.streamHIBR(context.Background(), "example.com", collect(&got, nil))
	if err != nil || !capped || len(calls) != 2 || len(got) != 1 {
		t.Fatalf("capped=%v err=%v records=%d calls=%v", capped, err, len(got), calls)
	}
	// A cap equal to the number of pages is not reported as truncation.
	calls = nil
	c = testClient(t, Options{HIBRKey: "synthetic-hibr", MaxPages: 4}, hibrPages(t, &calls))
	got = nil
	capped, err = c.streamHIBR(context.Background(), "example.com", collect(&got, nil))
	if err != nil || capped || len(calls) != 4 {
		t.Fatalf("capped=%v err=%v calls=%v", capped, err, calls)
	}
}

func TestHIBRMalformedResponses(t *testing.T) {
	for _, body := range []string{`{`, `[]`, `null`, `{}`, `{"success":false}`, `{"error":"SECRET"}`, `{"sources":[],"has_next":"false"}`, `{"sources":[null],"has_next":false}`, `{"sources":[{}],"has_next":false}`, `{"sources":[]}`} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, Options{HIBRKey: "synthetic-hibr"}, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			_, err := c.streamHIBR(context.Background(), "example.com", func(Record) bool { return true })
			if err == nil {
				t.Fatal("malformed response accepted")
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal("response body in error")
			}
		})
	}
}

func bhHandler(t *testing.T, limits *[]string, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bh" || r.Header.Get("Authorization") != "Bearer synthetic-bh" || r.URL.Query().Get("q") != "example.com" {
			t.Errorf("wrong BH request %s", r.URL)
		}
		*limits = append(*limits, r.URL.Query().Get("limit"))
		fmt.Fprint(w, body)
	}
}

func TestBreachHouseRecordsAndLimit(t *testing.T) {
	var limits []string
	body := `{"ransomware":[],"breaches":{"total":2,"results":[{"id":1,"website":"https://sub.example.com","name":"Indexed listing","group_name":"g","country":"ES","discovered":"2023-01-15 10:22:00.000000","password":"DO-NOT-RETURN","download_url":"https://blocked.invalid"},{"id":2,"website":"notexample.com"}]},"leads":[{"id":7,"post_title":"Lead for example.com"}],"stealers":[]}`
	c := testClient(t, Options{BHKey: "synthetic-bh"}, bhHandler(t, &limits, body))
	var got []Record
	omitted, err := c.streamBH(context.Background(), "example.com", 200, collect(&got, nil))
	if err != nil || omitted || len(got) != 2 || limits[0] != "200" {
		t.Fatalf("omitted=%v err=%v got=%+v limits=%v", omitted, err, got, limits)
	}
	if got[0].Product != bhProduct || got[0].Category != "breaches" || got[0].Title != "Indexed listing" || got[0].Country != "ES" ||
		got[0].Discovered == nil || got[0].Discovered.Hour() != 10 || got[1].Category != "leads" {
		t.Fatalf("mapping: %+v", got)
	}
	if fmt.Sprintf("%+v", got) != strings.ReplaceAll(fmt.Sprintf("%+v", got), "DO-NOT-RETURN", "") {
		t.Fatal("unapproved field returned")
	}
}

func TestBreachHouseStopAndOmitted(t *testing.T) {
	var limits []string
	body := `{"ransomware":{"total":9,"results":[{"id":1,"website":"example.com"},{"id":2,"website":"example.com"}]},"breaches":[{"id":3,"website":"example.com"}],"leads":[],"stealers":[]}`
	c := testClient(t, Options{BHKey: "synthetic-bh"}, bhHandler(t, &limits, body))
	var got []Record
	omitted, err := c.streamBH(context.Background(), "example.com", 2, collect(&got, nil))
	if err != nil || !omitted || len(got) != 3 || limits[0] != "2" {
		t.Fatalf("omitted=%v err=%v got=%d limits=%v", omitted, err, len(got), limits)
	}
	got = nil
	if _, err = c.streamBH(context.Background(), "example.com", 2, collect(&got, func(n int) bool { return n < 2 })); err != nil || len(got) != 2 {
		t.Fatalf("emit stop: err=%v got=%d", err, len(got))
	}
}

func TestRequestLimit(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	for _, tc := range []struct {
		in   *int64
		want int
	}{{nil, 200}, {i(1), 1}, {i(25), 25}, {i(199), 199}, {i(200), 200}, {i(201), 200}, {i(100000), 200}, {i(0), 200}, {i(-1), 200}} {
		if got := requestLimit(tc.in); got != tc.want {
			t.Errorf("requestLimit(%v)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestBreachHouseMalformed(t *testing.T) {
	for _, body := range []string{`{}`, `{"ransomware":[]}`, `{"ransomware":null,"breaches":[],"leads":[],"stealers":[]}`, `{"ransomware":{"results":[{}],"total":0},"breaches":[],"leads":[],"stealers":[]}`, `{"ransomware":{"results":[],"total":-1},"breaches":[],"leads":[],"stealers":[]}`} {
		c := testClient(t, Options{BHKey: "synthetic-bh"}, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := c.streamBH(context.Background(), "example.com", 200, func(Record) bool { return true }); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestBoundaryMatching(t *testing.T) {
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
}

func TestDomainNormalisation(t *testing.T) {
	for _, domain := range []string{"*.example.com", "example.com?x", "https://example.com", "user@example.com", "127.0.0.1", "-bad.com", "a..com", "example.com..", "example_com.com", "", "localhost"} {
		if _, err := normalizeDomain(domain); err == nil {
			t.Errorf("accepted %q", domain)
		}
	}
	if d, err := normalizeDomain("bücher.example"); err != nil || d != "xn--bcher-kva.example" {
		t.Fatalf("IDNA: %s %v", d, err)
	}
	if d, err := normalizeDomain(" EXAMPLE.com. "); err != nil || d != "example.com" {
		t.Fatalf("normalise: %s %v", d, err)
	}
}

func TestParseDiscovered(t *testing.T) {
	for in, want := range map[string]string{
		"2024-12-16 21:07:57.040833": "2024-12-16T21:07:57.040833Z",
		"2024-12-16 21:07:57":        "2024-12-16T21:07:57Z",
		"2025-06-08T16:41:23.807397": "2025-06-08T16:41:23.807397Z",
		"2025-06-08T16:41:23+02:00":  "2025-06-08T14:41:23Z",
		"2023-01-15":                 "2023-01-15T00:00:00Z",
	} {
		got := parseDiscovered(in)
		if got == nil || got.Format("2006-01-02T15:04:05.999999999Z07:00") != want {
			t.Errorf("parseDiscovered(%q)=%v want %s", in, got, want)
		}
	}
	for _, in := range []string{"", "yesterday", "15/01/2023"} {
		if parseDiscovered(in) != nil {
			t.Errorf("parseDiscovered(%q) should be nil", in)
		}
	}
}

func TestMissingKeyIsClearAndMakesNoRequest(t *testing.T) {
	c := testClient(t, Options{}, func(w http.ResponseWriter, r *http.Request) { t.Error("request made without a key") })
	_, err := c.streamHIBR(context.Background(), "example.com", func(Record) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "haveibeenransom_api_key") || !strings.Contains(err.Error(), "HIBR_API_KEY") {
		t.Fatalf("HIBR: %v", err)
	}
	_, err = c.streamBH(context.Background(), "example.com", 200, func(Record) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "breachhouse_api_key") || !strings.Contains(err.Error(), "BREACHHOUSE_API_KEY") {
		t.Fatalf("BH: %v", err)
	}
	if shouldRetryError(context.Background(), nil, nil, err) {
		t.Fatal("missing key must not be retried")
	}
}

func TestEachProductNeedsOnlyItsOwnKey(t *testing.T) {
	var hits atomic.Int32
	c := testClient(t, Options{HIBRKey: "synthetic-hibr"}, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if strings.HasPrefix(r.URL.Path, "/bh") {
			t.Error("BH called without a BH key")
		}
		fmt.Fprint(w, `{"sources":[{"id_source":1}],"has_next":false}`)
	})
	var got []Record
	if _, err := c.streamHIBR(context.Background(), "example.com", collect(&got, nil)); err != nil || len(got) != 1 {
		t.Fatalf("err=%v got=%d", err, len(got))
	}
}

func TestQuotaIsClearAndNotRetried(t *testing.T) {
	for _, product := range []string{"bh", "hibr"} {
		t.Run(product, func(t *testing.T) {
			var hits atomic.Int32
			c := testClient(t, Options{HIBRKey: "synthetic-hibr", BHKey: "synthetic-bh"}, func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(429)
				fmt.Fprint(w, `{"error":"Daily limit exceeded","message":"synthetic-bh synthetic-hibr RESPONSE-SECRET","daily_limit":2000}`)
			})
			var err error
			if product == "bh" {
				_, err = c.streamBH(context.Background(), "example.com", 200, func(Record) bool { return true })
			} else {
				_, err = c.streamHIBR(context.Background(), "example.com", func(Record) bool { return true })
			}
			var re *requestError
			if !errors.As(err, &re) || !re.Quota() || re.Retryable() || hits.Load() != 1 {
				t.Fatalf("err=%v hits=%d", err, hits.Load())
			}
			if !strings.Contains(err.Error(), "daily request budget is used up") || !strings.Contains(err.Error(), "429") {
				t.Fatalf("unclear message: %v", err)
			}
			if shouldRetryError(context.Background(), nil, nil, fmt.Errorf("wrapped: %w", err)) {
				t.Fatal("429 must not be retried")
			}
			assertNoLeak(t, err)
		})
	}
}

func assertNoLeak(t *testing.T, err error) {
	t.Helper()
	for _, s := range []string{"synthetic-bh", "synthetic-hibr", "SECRET", "Bearer", "Daily limit", "daily_limit"} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("error leaks %q: %v", s, err)
		}
	}
}

func TestStatusClassificationAndNoLeaks(t *testing.T) {
	for status, retry := range map[int]bool{400: false, 401: false, 403: false, 404: false, 302: false, 429: false, 500: true, 502: true, 503: true, 504: true} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, Options{HIBRKey: "synthetic-hibr"}, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://evil.invalid/")
				w.WriteHeader(status)
				fmt.Fprint(w, "synthetic-hibr RESPONSE-SECRET")
			})
			_, err := c.streamHIBR(context.Background(), "example.com", func(Record) bool { return true })
			if err == nil {
				t.Fatal("status accepted")
			}
			if got := shouldRetryError(context.Background(), nil, nil, fmt.Errorf("ctx: %w", err)); got != retry {
				t.Fatalf("retry=%v want %v (%v)", got, retry, err)
			}
			if !strings.Contains(err.Error(), fmt.Sprint(status)) {
				t.Fatalf("status missing: %v", err)
			}
			assertNoLeak(t, err)
		})
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer target.Close()
	c := testClient(t, Options{HIBRKey: "synthetic-hibr"}, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	if _, err := c.streamHIBR(context.Background(), "example.com", func(Record) bool { return true }); err == nil || calls.Load() != 0 {
		t.Fatalf("redirect followed or accepted: %v calls=%d", err, calls.Load())
	}
}

func TestResponseSizeCap(t *testing.T) {
	c := testClient(t, Options{HIBRKey: "synthetic-hibr"}, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat(" ", maxBody+1)) })
	_, err := c.streamHIBR(context.Background(), "example.com", func(Record) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "size limit") || shouldRetryError(context.Background(), nil, nil, err) {
		t.Fatalf("%v", err)
	}
}

func TestTransientNetworkErrorsAreRetryable(t *testing.T) {
	// Connection dropped mid-request.
	drop := testClient(t, Options{HIBRKey: "synthetic-hibr"}, func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	})
	_, err := drop.streamHIBR(context.Background(), "example.com", func(Record) bool { return true })
	if err == nil || !shouldRetryError(context.Background(), nil, nil, err) {
		t.Fatalf("dropped connection should be retryable: %v", err)
	}
	assertNoLeak(t, err)
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("address in error: %v", err)
	}

	// Timeout.
	slow := testClient(t, Options{HIBRKey: "synthetic-hibr"}, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	slow.http.Timeout = 50 * time.Millisecond
	_, err = slow.streamHIBR(context.Background(), "example.com", func(Record) bool { return true })
	if err == nil || !shouldRetryError(context.Background(), nil, nil, err) {
		t.Fatalf("timeout should be retryable: %v", err)
	}

	// Connection refused.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	refused := newClient(Options{HIBRKey: "synthetic-hibr"})
	refused.hibr = "http://" + addr + "/hibr/"
	_, err = refused.streamHIBR(context.Background(), "example.com", func(Record) bool { return true })
	if err == nil || !shouldRetryError(context.Background(), nil, nil, err) {
		t.Fatalf("refused should be retryable: %v", err)
	}
}

func TestCancellationIsNotRetried(t *testing.T) {
	c := testClient(t, Options{HIBRKey: "synthetic-hibr"}, func(w http.ResponseWriter, r *http.Request) { t.Error("request after cancel") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.streamHIBR(ctx, "example.com", func(Record) bool { return true })
	if !errors.Is(err, context.Canceled) || shouldRetryError(ctx, nil, nil, err) {
		t.Fatalf("%v", err)
	}
}

func TestRetryClassification(t *testing.T) {
	if shouldRetryError(context.Background(), nil, nil, errors.New("plain")) {
		t.Fatal("plain errors are not retried")
	}
	if shouldRetryError(context.Background(), nil, nil, nil) {
		t.Fatal("nil is not retried")
	}
	if !shouldRetryError(context.Background(), nil, nil, statusError(bhProduct, 503)) {
		t.Fatal("5xx is retried")
	}
	// After rows were streamed a retry would repeat them.
	s := &streamer{send: func(Record) {}, more: func() bool { return true }}
	err := fmt.Errorf("outer: %w", statusError(bhProduct, 503))
	if !shouldRetryError(context.Background(), nil, nil, s.wrap(err)) {
		t.Fatal("5xx before any row is retried")
	}
	s.emit(Record{})
	if shouldRetryError(context.Background(), nil, nil, s.wrap(err)) {
		t.Fatal("5xx after rows were streamed must not be retried")
	}
	if !errors.Is(s.wrap(err), err) {
		t.Fatal("wrapping must keep the cause")
	}
	if s.emit(Record{}) != true {
		t.Fatal("emit must report more rows needed")
	}
	s.more = func() bool { return false }
	if s.emit(Record{}) {
		t.Fatal("emit must stop when no rows remain")
	}
}

func TestTransientClassifier(t *testing.T) {
	for err, want := range map[error]bool{
		nil:                                 false,
		context.Canceled:                    false,
		context.DeadlineExceeded:            true,
		errors.New("x509: bad certificate"): false,
		fmt.Errorf("w: %w", net.ErrClosed):  false,
		&net.DNSError{IsTimeout: true}:      true,
		&net.DNSError{IsNotFound: true}:     false,
	} {
		if got := isTransientNetworkError(err); got != want {
			t.Errorf("isTransientNetworkError(%v)=%v want %v", err, got, want)
		}
	}
}
