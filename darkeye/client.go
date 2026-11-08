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
)

type Options struct {
	HIBRKey  string
	BHKey    string
	MaxPages int
	Limit    int
}

type Record struct {
	Product    string `json:"product"`
	Category   string `json:"category"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Website    string `json:"website"`
	Group      string `json:"group"`
	Country    string `json:"country"`
	Discovered string `json:"discovered"`
}

type ProductError struct {
	Product string `json:"product"`
	Message string `json:"message"`
}

type Report struct {
	Provider  string         `json:"provider"`
	Domain    string         `json:"domain"`
	Records   []Record       `json:"records"`
	Products  []string       `json:"products"`
	Errors    []ProductError `json:"errors"`
	Truncated bool           `json:"truncated"`
	Complete  bool           `json:"complete"`
}

type client struct {
	http *http.Client
	hibr string
	bh   string
}

func newClient() *client {
	return &client{http: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}, hibr: hibrURL, bh: bhURL}
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

func (o Options) validate() error {
	if o.HIBRKey == "" && o.BHKey == "" {
		return errors.New("configure haveibeenransom_api_key or breachhouse_api_key")
	}
	if o.MaxPages < 1 || o.MaxPages > 50 || o.Limit < 1 || o.Limit > 200 {
		return errors.New("max_pages must be 1..50 and limit must be 1..200")
	}
	return nil
}

func (c *client) lookup(ctx context.Context, domain string, o Options) (Report, error) {
	r := Report{Provider: "DarkEye", Records: []Record{}, Products: []string{}, Errors: []ProductError{}}
	if err := o.validate(); err != nil {
		return r, err
	}
	var err error
	r.Domain, err = normalizeDomain(domain)
	if err != nil {
		return r, err
	}
	for _, product := range []struct {
		name, key string
		fetch     func(context.Context, string, Options) ([]Record, bool, error)
	}{
		{hibrProduct, o.HIBRKey, c.fetchHIBR}, {bhProduct, o.BHKey, c.fetchBH},
	} {
		if product.key == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return r, err
		}
		records, truncated, err := product.fetch(ctx, r.Domain, o)
		r.Products = append(r.Products, product.name)
		r.Records = append(r.Records, records...)
		r.Truncated = r.Truncated || truncated
		if err != nil {
			r.Errors = append(r.Errors, ProductError{product.name, err.Error()})
		}
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	r.Complete = len(r.Errors) == 0 && !r.Truncated
	return r, nil
}

func (c *client) get(ctx context.Context, endpoint, key string) (map[string]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "steampipe-plugin-darkeye/0.1.0")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("request failed or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return nil, errors.New("could not read response")
	}
	if len(data) > maxBody {
		return nil, errors.New("response exceeds size limit")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(data, &body) != nil || body == nil {
		return nil, errors.New("invalid JSON response")
	}
	if _, ok := body["error"]; ok {
		return nil, errors.New("unsuccessful API response")
	}
	if b, ok := body["success"]; ok && string(b) == "false" {
		return nil, errors.New("unsuccessful API response")
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

func makeRecord(product, category string, item map[string]json.RawMessage) Record {
	discovered := textField(item, "discovered")
	if len(discovered) > 10 {
		discovered = discovered[:10]
	}
	return Record{Product: product, Category: category, ID: textField(item, "id_source", "victim_id", "id"),
		Title: textField(item, "post_title", "name"), Website: textField(item, "website"),
		Group: textField(item, "group_name"), Country: textField(item, "country"), Discovered: discovered}
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

func (c *client) fetchHIBR(ctx context.Context, domain string, o Options) ([]Record, bool, error) {
	records := []Record{}
	seen := map[string]bool{}
	for page := 1; page <= o.MaxPages; page++ {
		endpoint := c.hibr + domain
		if page > 1 {
			endpoint += "/" + strconv.Itoa(page)
		}
		body, err := c.get(ctx, endpoint, o.HIBRKey)
		if err != nil {
			return records, false, err
		}
		raw, ok := body["sources"]
		if !ok {
			raw = body["results"]
		}
		items, err := parseItems(raw)
		if err != nil {
			return records, false, err
		}
		next, ok := body["has_next"]
		if !ok {
			var pagination map[string]json.RawMessage
			if json.Unmarshal(body["pagination"], &pagination) != nil {
				return records, false, errors.New("invalid pagination")
			}
			next = pagination["has_next"]
		}
		if string(next) != "true" && string(next) != "false" {
			return records, false, errors.New("invalid pagination")
		}
		for _, item := range items {
			category := map[string]string{"ransomware_leak": "ransomware", "traditional_breach": "breaches"}[textField(item, "leak_type")]
			if category == "" {
				category = "leaks"
			}
			r := makeRecord(hibrProduct, category, item)
			if r.ID == "" && r.Title == "" && r.Website == "" {
				return records, false, errors.New("invalid metadata record")
			}
			key := recordKey(r)
			if !seen[key] {
				records = append(records, r)
				seen[key] = true
			}
		}
		if string(next) == "false" {
			return records, false, nil
		}
	}
	return records, true, nil
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

func (c *client) fetchBH(ctx context.Context, domain string, o Options) ([]Record, bool, error) {
	params := url.Values{"q": {domain}, "limit": {strconv.Itoa(o.Limit)}}
	body, err := c.get(ctx, c.bh+"?"+params.Encode(), o.BHKey)
	if err != nil {
		return nil, false, err
	}
	records := []Record{}
	seen := map[string]bool{}
	truncated := false
	for _, category := range []string{"ransomware", "breaches", "leads", "stealers"} {
		raw, ok := body[category]
		if !ok {
			return records, truncated, errors.New("missing search category")
		}
		var section map[string]json.RawMessage
		total := -1
		if json.Unmarshal(raw, &section) == nil && section != nil {
			raw = section["results"]
			if b, ok := section["total"]; ok {
				if json.Unmarshal(b, &total) != nil || total < 0 {
					return records, truncated, errors.New("invalid search total")
				}
			}
		}
		items, err := parseItems(raw)
		if err != nil {
			return records, truncated, err
		}
		if total >= 0 && total < len(items) {
			return records, truncated, errors.New("invalid search total")
		}
		truncated = truncated || (total > len(items)) || (total < 0 && len(items) >= o.Limit)
		for _, item := range items {
			r := makeRecord(bhProduct, category, item)
			if !matchesDomain(r, domain) {
				continue
			}
			key := recordKey(r)
			if !seen[key] {
				records = append(records, r)
				seen[key] = true
			}
		}
	}
	return records, truncated, nil
}
