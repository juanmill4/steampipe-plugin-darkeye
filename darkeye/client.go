package darkeye

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

const (
	hibrProduct = "Have I Been Ransomed?"
	bhProduct   = "Breach House"
	hibrURL     = "https://haveibeenransom.com/api/metadata/domain/"
	bhURL       = "https://breach.house/api/v1/search"
	maxBody     = 4 << 20

	// maxBHLimit is the documented maximum of the Breach House search "limit"
	// parameter (results per source).
	maxBHLimit = 200
)

// Record is one metadata listing returned by a product.
type Record struct {
	Product    string     `json:"product"`
	Category   string     `json:"category"`
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Website    string     `json:"website"`
	Group      string     `json:"group_name"`
	Country    string     `json:"country"`
	Discovered *time.Time `json:"discovered"`
}

type client struct {
	http     *http.Client
	hibr     string
	bh       string
	hibrKey  string
	bhKey    string
	maxPages int
}

func newClient(o Options) *client {
	return &client{http: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}, hibr: hibrURL, bh: bhURL, hibrKey: o.HIBRKey, bhKey: o.BHKey, maxPages: o.MaxPages}
}

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var digits = regexp.MustCompile(`^[0-9]+$`)
var hostTokens = regexp.MustCompile(`[a-z0-9_.-]+`)

func normalizeDomain(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	ascii, err := idna.Lookup.ToASCII(value)
	labels := strings.Split(ascii, ".")
	if err != nil || len(ascii) > 253 || len(labels) < 2 || digits.MatchString(labels[len(labels)-1]) {
		return "", errors.New("a plain domain name is required")
	}
	for _, label := range labels {
		if !labelPattern.MatchString(label) {
			return "", errors.New("a plain domain name is required")
		}
	}
	return ascii, nil
}

// get performs one authenticated GET. Errors never include the response body,
// the URL or the key.
func (c *client) get(ctx context.Context, product, endpoint, key string) (map[string]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, permanentError(product, "invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, networkError(ctx, product, err, "request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, statusError(product, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return nil, networkError(ctx, product, err, "could not read response")
	}
	if len(data) > maxBody {
		return nil, permanentError(product, "response exceeds size limit")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(data, &body) != nil || body == nil {
		return nil, permanentError(product, "invalid JSON response")
	}
	if _, ok := body["error"]; ok {
		return nil, permanentError(product, "unsuccessful API response")
	}
	if b, ok := body["success"]; ok && string(b) == "false" {
		return nil, permanentError(product, "unsuccessful API response")
	}
	return body, nil
}

func textField(m map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var s string
		if json.Unmarshal(m[key], &s) != nil {
			var n json.Number
			if len(m[key]) == 0 || string(m[key]) == "null" || json.Unmarshal(m[key], &n) != nil {
				continue
			}
			s = n.String()
		}
		s = strings.Join(strings.Fields(s), " ")
		runes := []rune(s)
		if len(runes) > 512 {
			s = string(runes[:512])
		}
		if s != "" {
			return s
		}
	}
	return ""
}

// discoveredLayouts are the date formats seen in the products' "discovered"
// field, e.g. "2024-12-16 21:07:57.040833". Values carry no zone and are read as UTC.
var discoveredLayouts = []string{"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999", time.RFC3339Nano, "2006-01-02"}

// parseDiscovered converts a "discovered" value to a UTC timestamp; unparseable
// or empty values become nil (NULL).
func parseDiscovered(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range discoveredLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func makeRecord(product, category string, item map[string]json.RawMessage) Record {
	return Record{Product: product, Category: category, ID: textField(item, "id_source", "victim_id", "id"),
		Title: textField(item, "post_title", "name"), Website: textField(item, "website"),
		Group: textField(item, "group_name"), Country: textField(item, "country"),
		Discovered: parseDiscovered(textField(item, "discovered"))}
}

func recordKey(r Record) string {
	if r.ID != "" {
		return r.Category + ":" + r.ID
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func parseItems(raw json.RawMessage) ([]map[string]json.RawMessage, error) {
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, errors.New("invalid records")
	}
	for _, item := range items {
		if item == nil {
			return nil, errors.New("invalid record")
		}
	}
	return items, nil
}

func missingKey(product, argument, env string) error {
	return fmt.Errorf("%s needs an API key: set %s in the connection configuration or the %s environment variable", product, argument, env)
}

// streamHIBR follows the Have I Been Ransomed? metadata pagination until the API
// reports no more pages. emit receives each record once and returns false when
// no more rows are needed (a SQL limit was reached), which stops pagination
// before the next request. It returns capped=true when max_pages ended the walk
// while the API still had more pages.
func (c *client) streamHIBR(ctx context.Context, domain string, emit func(Record) bool) (capped bool, err error) {
	if c.hibrKey == "" {
		return false, missingKey(hibrProduct, "haveibeenransom_api_key", "HIBR_API_KEY")
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		if c.maxPages > 0 && page > c.maxPages {
			return true, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		endpoint := c.hibr + domain
		if page > 1 {
			endpoint += "/" + strconv.Itoa(page)
		}
		body, err := c.get(ctx, hibrProduct, endpoint, c.hibrKey)
		if err != nil {
			return false, err
		}
		raw, ok := body["sources"]
		if !ok {
			raw = body["results"]
		}
		items, err := parseItems(raw)
		if err != nil {
			return false, permanentError(hibrProduct, err.Error())
		}
		next, ok := body["has_next"]
		if !ok {
			var pagination map[string]json.RawMessage
			if json.Unmarshal(body["pagination"], &pagination) != nil {
				return false, permanentError(hibrProduct, "invalid pagination")
			}
			next = pagination["has_next"]
		}
		if string(next) != "true" && string(next) != "false" {
			return false, permanentError(hibrProduct, "invalid pagination")
		}
		for _, item := range items {
			category := map[string]string{"ransomware_leak": "ransomware", "traditional_breach": "breaches"}[textField(item, "leak_type")]
			if category == "" {
				category = "leaks"
			}
			r := makeRecord(hibrProduct, category, item)
			if r.ID == "" && r.Title == "" && r.Website == "" {
				return false, permanentError(hibrProduct, "invalid metadata record")
			}
			key := recordKey(r)
			if seen[key] {
				continue
			}
			seen[key] = true
			if !emit(r) {
				return false, nil
			}
		}
		if string(next) == "false" {
			return false, nil
		}
	}
}

func matchesDomain(r Record, domain string) bool {
	match := func(host string) bool { return host == domain || strings.HasSuffix(host, "."+domain) }
	if r.Website != "" {
		value := r.Website
		if !strings.Contains(value, "://") {
			value = "https://" + value
		}
		u, err := url.Parse(value)
		if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return false
		}
		host, err := normalizeDomain(u.Hostname())
		return err == nil && match(host)
	}
	for _, token := range hostTokens.FindAllString(strings.ToLower(r.Title), -1) {
		host, err := normalizeDomain(token)
		if err == nil && match(host) {
			return true
		}
	}
	return false
}

// requestLimit is the Breach House "limit" for a query: the SQL limit when it is
// smaller than the documented maximum of 200, otherwise 200.
func requestLimit(sqlLimit *int64) int {
	if sqlLimit != nil && *sqlLimit > 0 && *sqlLimit < maxBHLimit {
		return int(*sqlLimit)
	}
	return maxBHLimit
}

// streamBH makes the single Breach House search request (the documented search
// endpoint has no offset, so there is no second page) and emits the records whose
// website, or title hostname, matches the domain. omitted=true means the API
// reported more results than the requested limit returned.
func (c *client) streamBH(ctx context.Context, domain string, limit int, emit func(Record) bool) (omitted bool, err error) {
	if c.bhKey == "" {
		return false, missingKey(bhProduct, "breachhouse_api_key", "BREACHHOUSE_API_KEY")
	}
	params := url.Values{"q": {domain}, "limit": {strconv.Itoa(limit)}}
	body, err := c.get(ctx, bhProduct, c.bh+"?"+params.Encode(), c.bhKey)
	if err != nil {
		return false, err
	}
	seen := map[string]bool{}
	for _, category := range []string{"ransomware", "breaches", "leads", "stealers"} {
		raw, ok := body[category]
		if !ok {
			return omitted, permanentError(bhProduct, "missing search category")
		}
		var section map[string]json.RawMessage
		total := -1
		if json.Unmarshal(raw, &section) == nil && section != nil {
			raw = section["results"]
			if b, ok := section["total"]; ok {
				if json.Unmarshal(b, &total) != nil || total < 0 {
					return omitted, permanentError(bhProduct, "invalid search total")
				}
			}
		}
		items, err := parseItems(raw)
		if err != nil {
			return omitted, permanentError(bhProduct, err.Error())
		}
		if total >= 0 && total < len(items) {
			return omitted, permanentError(bhProduct, "invalid search total")
		}
		omitted = omitted || total > len(items) || (total < 0 && len(items) >= limit)
		for _, item := range items {
			r := makeRecord(bhProduct, category, item)
			if !matchesDomain(r, domain) {
				continue
			}
			key := recordKey(r)
			if seen[key] {
				continue
			}
			seen[key] = true
			if !emit(r) {
				return omitted, nil
			}
		}
	}
	return omitted, nil
}
