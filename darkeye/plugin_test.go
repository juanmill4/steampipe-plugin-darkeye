package darkeye

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/turbot/steampipe-plugin-sdk/v6/anywhere"
	"github.com/turbot/steampipe-plugin-sdk/v6/grpc"
	"github.com/turbot/steampipe-plugin-sdk/v6/grpc/proto"
	"github.com/turbot/steampipe-plugin-sdk/v6/plugin"
)

// origTransport is the real default transport, captured before any test swaps it.
var origTransport = http.DefaultTransport

// rewriteTransport sends the products' real hostnames to a local test server so
// the whole plugin (SDK included) runs offline.
type rewriteTransport struct{ target *url.URL }

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	c.URL.Scheme, c.URL.Host = r.target.Scheme, r.target.Host
	return origTransport.RoundTrip(c)
}

type fakeAPI struct {
	mu       sync.Mutex
	hibrPath []string
	bhQuery  []url.Values
	handler  func(w http.ResponseWriter, r *http.Request, attempt int)
	attempts map[string]int
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.attempts[r.URL.Path]++
	attempt := f.attempts[r.URL.Path]
	if strings.HasPrefix(r.URL.Path, "/api/v1/search") {
		f.bhQuery = append(f.bhQuery, r.URL.Query())
	} else {
		f.hibrPath = append(f.hibrPath, r.URL.Path)
	}
	f.mu.Unlock()
	f.handler(w, r, attempt)
}

func startPlugin(t *testing.T, config string, handler func(w http.ResponseWriter, r *http.Request, attempt int)) (*grpc.PluginServer, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{handler: handler, attempts: map[string]int{}}
	s := httptest.NewServer(api)
	t.Cleanup(s.Close)
	u, _ := url.Parse(s.URL)
	http.DefaultTransport = rewriteTransport{target: u}
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	srv := plugin.Server(&plugin.ServeOpts{PluginFunc: Plugin})
	res, err := srv.SetAllConnectionConfigs(&proto.SetAllConnectionConfigsRequest{Configs: []*proto.ConnectionConfig{
		{Connection: "darkeye", Plugin: "juanmill4/darkeye", PluginShortName: "darkeye", Config: config},
	}})
	if err != nil || len(res.FailedConnections) > 0 {
		t.Fatalf("connection config: %v %v", err, res)
	}
	return srv, api
}

func query(t *testing.T, srv *grpc.PluginServer, table, domain string, limit *int64) ([]map[string]*proto.Column, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	qc := &proto.QueryContext{
		Columns: []string{"domain", "product", "category", "id", "title", "website", "group_name", "country", "discovered"},
		Quals: map[string]*proto.Quals{"domain": {Quals: []*proto.Qual{{
			FieldName: "domain",
			Operator:  &proto.Qual_StringValue{StringValue: "="},
			Value:     &proto.QualValue{Value: &proto.QualValue_StringValue{StringValue: domain}},
		}}}},
	}
	ecd := &proto.ExecuteConnectionData{}
	if limit != nil {
		qc.Limit = &proto.NullableInt{Value: *limit}
		ecd.Limit = &proto.NullableInt{Value: *limit}
	}
	stream := anywhere.NewLocalPluginStream(ctx)
	srv.CallExecuteAsync(&proto.ExecuteRequest{
		Table: table, QueryContext: qc, Connection: "darkeye", CallId: fmt.Sprint(time.Now().UnixNano()),
		ExecuteConnectionData: map[string]*proto.ExecuteConnectionData{"darkeye": ecd},
	}, stream)
	var rows []map[string]*proto.Column
	for {
		resp, err := stream.Recv()
		if err != nil {
			return rows, err
		}
		if resp == nil {
			return rows, nil
		}
		if resp.Row != nil {
			rows = append(rows, resp.Row.Columns)
		}
	}
}

func hibrPage(page int, ids ...string) string {
	items := make([]string, len(ids))
	for i, id := range ids {
		items[i] = fmt.Sprintf(`{"id_source":%s,"post_title":"Listing %s","website":"example.com","group_name":"grp","country":"US","leak_type":"ransomware_leak","discovered":"2024-12-16 21:07:57.040833"}`, id, id)
	}
	return fmt.Sprintf(`{"success":true,"results":[%s],"pagination":{"current_page":%d,"has_next":%t}}`, strings.Join(items, ","), page, page < 3)
}

func hibrHandler(w http.ResponseWriter, r *http.Request, _ int) {
	switch r.URL.Path {
	case "/api/metadata/domain/example.com":
		fmt.Fprint(w, hibrPage(1, "1", "2"))
	case "/api/metadata/domain/example.com/2":
		fmt.Fprint(w, hibrPage(2, "3", "4"))
	case "/api/metadata/domain/example.com/3":
		fmt.Fprint(w, hibrPage(3, "5"))
	default:
		w.WriteHeader(404)
	}
}

const bothKeys = "haveibeenransom_api_key = \"synthetic-hibr\"\nbreachhouse_api_key = \"synthetic-bh\"\n"

func TestSchema(t *testing.T) {
	srv, _ := startPlugin(t, bothKeys, hibrHandler)
	res, err := srv.GetSchema(&proto.GetSchemaRequest{Connection: "darkeye"})
	if err != nil {
		t.Fatal(err)
	}
	tables := res.Schema.Schema
	if len(tables) != 2 {
		t.Fatalf("tables: %d", len(tables))
	}
	want := map[string]proto.ColumnType{"domain": proto.ColumnType_STRING, "product": proto.ColumnType_STRING, "category": proto.ColumnType_STRING,
		"id": proto.ColumnType_STRING, "title": proto.ColumnType_STRING, "website": proto.ColumnType_STRING, "group_name": proto.ColumnType_STRING,
		"country": proto.ColumnType_STRING, "discovered": proto.ColumnType_TIMESTAMP, "sp_connection_name": proto.ColumnType_STRING, "sp_ctx": proto.ColumnType_JSON, "_ctx": proto.ColumnType_JSON}
	for _, name := range []string{"darkeye_haveibeenransom_record", "darkeye_breachhouse_record"} {
		table := tables[name]
		if table == nil {
			t.Fatalf("missing table %s", name)
		}
		required := []string{}
		for _, k := range table.ListCallKeyColumnList {
			if k.Require == plugin.Required {
				required = append(required, k.Name)
			}
		}
		if len(required) != 1 || required[0] != "domain" {
			t.Fatalf("%s required key columns: %v", name, required)
		}
		got := map[string]proto.ColumnType{}
		for _, c := range table.Columns {
			got[c.Name] = c.Type
		}
		for col, typ := range want {
			if g, ok := got[col]; ok && g != typ {
				t.Errorf("%s.%s type %v want %v", name, col, g, typ)
			}
		}
		for _, col := range []string{"domain", "product", "category", "id", "title", "website", "group_name", "country", "discovered", "_ctx"} {
			if _, ok := got[col]; !ok {
				t.Errorf("%s missing column %s", name, col)
			}
		}
		for _, col := range []string{"provider", "products", "truncated", "complete", "errors", "records"} {
			if _, ok := got[col]; ok {
				t.Errorf("%s still has column %s", name, col)
			}
		}
	}
}

func TestHIBRTableEndToEnd(t *testing.T) {
	srv, api := startPlugin(t, bothKeys, hibrHandler)
	rows, err := query(t, srv, "darkeye_haveibeenransom_record", "example.com", nil)
	if err != nil || len(rows) != 5 || len(api.hibrPath) != 3 {
		t.Fatalf("rows=%d err=%v paths=%v", len(rows), err, api.hibrPath)
	}
	byID := map[string]map[string]*proto.Column{}
	for _, row := range rows {
		byID[row["id"].GetStringValue()] = row
	}
	r := byID["1"]
	if len(byID) != 5 || r == nil || r["product"].GetStringValue() != hibrProduct || r["category"].GetStringValue() != "ransomware" ||
		r["domain"].GetStringValue() != "example.com" || r["group_name"].GetStringValue() != "grp" {
		t.Fatalf("row: %v", r)
	}
	ts := r["discovered"].GetTimestampValue()
	if ts == nil || ts.AsTime().Format(time.RFC3339Nano) != "2024-12-16T21:07:57.040833Z" {
		t.Fatalf("discovered must be a timestamp: %v", r["discovered"])
	}
}

func TestHIBRSQLLimitStopsPagination(t *testing.T) {
	srv, api := startPlugin(t, bothKeys, hibrHandler)
	limit := int64(2)
	rows, err := query(t, srv, "darkeye_haveibeenransom_record", "example.com", &limit)
	if err != nil || len(rows) != 2 || len(api.hibrPath) != 1 {
		t.Fatalf("rows=%d err=%v paths=%v", len(rows), err, api.hibrPath)
	}
	// 3 rows need a second page.
	api.hibrPath = nil
	limit = 3
	rows, err = query(t, srv, "darkeye_haveibeenransom_record", "example.com", &limit)
	if err != nil || len(rows) != 3 || len(api.hibrPath) != 2 {
		t.Fatalf("rows=%d err=%v paths=%v", len(rows), err, api.hibrPath)
	}
}

func TestHIBRMaxPagesConfig(t *testing.T) {
	srv, api := startPlugin(t, bothKeys+"max_pages = 2\n", hibrHandler)
	rows, err := query(t, srv, "darkeye_haveibeenransom_record", "example.com", nil)
	if err != nil || len(rows) != 4 || len(api.hibrPath) != 2 {
		t.Fatalf("rows=%d err=%v paths=%v", len(rows), err, api.hibrPath)
	}
}

const bhBody = `{"ransomware":{"total":2,"results":[{"id":"abc","post_title":"Acme","website":"example.com","group_name":"g","country":"ES","discovered":"2023-01-15 10:22:00.000000"}]},"breaches":[{"id":9,"website":"other.test"}],"leads":[],"stealers":[]}`

func TestBreachHouseTableEndToEndAndLimit(t *testing.T) {
	srv, api := startPlugin(t, bothKeys, func(w http.ResponseWriter, r *http.Request, _ int) { fmt.Fprint(w, bhBody) })
	rows, err := query(t, srv, "darkeye_breachhouse_record", "example.com", nil)
	if err != nil || len(rows) != 1 || api.bhQuery[0].Get("limit") != "200" || api.bhQuery[0].Get("q") != "example.com" {
		t.Fatalf("rows=%d err=%v q=%v", len(rows), err, api.bhQuery)
	}
	if rows[0]["product"].GetStringValue() != bhProduct || rows[0]["category"].GetStringValue() != "ransomware" || rows[0]["discovered"].GetTimestampValue() == nil {
		t.Fatalf("row: %v", rows[0])
	}
	for limit, want := range map[int64]string{7: "7", 200: "200", 5000: "200"} {
		l := limit
		if _, err = query(t, srv, "darkeye_breachhouse_record", "example.com", &l); err != nil {
			t.Fatal(err)
		}
		if got := api.bhQuery[len(api.bhQuery)-1].Get("limit"); got != want {
			t.Errorf("SQL limit %d -> request limit %s want %s", limit, got, want)
		}
	}
}

func TestBreachHouse429IsAClearErrorAndNotRetried(t *testing.T) {
	srv, api := startPlugin(t, bothKeys, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":"rate","message":"synthetic-bh BODY-SECRET"}`)
	})
	_, err := query(t, srv, "darkeye_breachhouse_record", "example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "daily request budget is used up") || !strings.Contains(err.Error(), "darkeye_breachhouse_record") {
		t.Fatalf("err=%v", err)
	}
	if len(api.bhQuery) != 1 {
		t.Fatalf("429 was retried: %d requests", len(api.bhQuery))
	}
	assertNoLeak(t, err)
}

func TestServerErrorIsRetriedBySDK(t *testing.T) {
	srv, api := startPlugin(t, bothKeys, func(w http.ResponseWriter, r *http.Request, attempt int) {
		if attempt == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, bhBody)
	})
	rows, err := query(t, srv, "darkeye_breachhouse_record", "example.com", nil)
	if err != nil || len(rows) != 1 || len(api.bhQuery) != 2 {
		t.Fatalf("rows=%d err=%v requests=%d", len(rows), err, len(api.bhQuery))
	}
}

func TestFailureAfterRowsAreStreamedIsNotRetried(t *testing.T) {
	srv, api := startPlugin(t, bothKeys, func(w http.ResponseWriter, r *http.Request, _ int) {
		if r.URL.Path == "/api/metadata/domain/example.com/2" {
			w.WriteHeader(503)
			return
		}
		hibrHandler(w, r, 0)
	})
	rows, err := query(t, srv, "darkeye_haveibeenransom_record", "example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	if len(api.hibrPath) != 2 {
		t.Fatalf("retry would repeat rows: %v", api.hibrPath)
	}
}

func TestTablesFailIndependentlyAndNeedOnlyOwnKey(t *testing.T) {
	// Only a Breach House key: the HIBR table reports a clear missing-key error, BH works.
	srv, _ := startPlugin(t, "breachhouse_api_key = \"synthetic-bh\"\n", func(w http.ResponseWriter, r *http.Request, _ int) { fmt.Fprint(w, bhBody) })
	if _, err := query(t, srv, "darkeye_haveibeenransom_record", "example.com", nil); err == nil || !strings.Contains(err.Error(), "haveibeenransom_api_key") {
		t.Fatalf("err=%v", err)
	}
	if rows, err := query(t, srv, "darkeye_breachhouse_record", "example.com", nil); err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	// Only a HIBR key.
	srv, _ = startPlugin(t, "haveibeenransom_api_key = \"synthetic-hibr\"\n", hibrHandler)
	if _, err := query(t, srv, "darkeye_breachhouse_record", "example.com", nil); err == nil || !strings.Contains(err.Error(), "breachhouse_api_key") {
		t.Fatalf("err=%v", err)
	}
	if rows, err := query(t, srv, "darkeye_haveibeenransom_record", "example.com", nil); err != nil || len(rows) != 5 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
}

func TestInvalidDomainAndConfigAreErrors(t *testing.T) {
	srv, api := startPlugin(t, bothKeys, hibrHandler)
	if _, err := query(t, srv, "darkeye_haveibeenransom_record", "https://example.com", nil); err == nil {
		t.Fatal("URL accepted")
	}
	if len(api.hibrPath) != 0 {
		t.Fatal("invalid domain made a request")
	}
	srv, api = startPlugin(t, bothKeys+"max_pages = 0\n", hibrHandler)
	_, err := query(t, srv, "darkeye_haveibeenransom_record", "example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "max_pages") || len(api.hibrPath) != 0 {
		t.Fatalf("err=%v", err)
	}
}
