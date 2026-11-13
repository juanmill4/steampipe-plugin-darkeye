package darkeye

import (
	"context"
	"fmt"

	"github.com/turbot/steampipe-plugin-sdk/v6/grpc/proto"
	"github.com/turbot/steampipe-plugin-sdk/v6/plugin"
	"github.com/turbot/steampipe-plugin-sdk/v6/plugin/transform"
)

const clientCacheKey = "darkeye-client"

// Plugin returns the DarkEye plugin: one plugin, one table per product.
func Plugin(_ context.Context) *plugin.Plugin {
	return &plugin.Plugin{
		Name: "steampipe-plugin-darkeye",
		ConnectionConfigSchema: &plugin.ConnectionConfigSchema{
			NewInstance: func() interface{} { return &Config{} },
		},
		DefaultTransform: transform.FromJSONTag(),
		// Retries apply to 5xx responses and transient network errors only; a 429
		// means the daily request budget is used up and is returned as an error.
		DefaultRetryConfig: &plugin.RetryConfig{
			ShouldRetryErrorFunc: shouldRetryError,
			MaxAttempts:          3,
			BackoffAlgorithm:     "Exponential",
			RetryInterval:        500,
			CappedDuration:       5000,
			MaxDuration:          20000,
		},
		TableMap: map[string]*plugin.Table{
			"darkeye_haveibeenransom_record": tableHaveIBeenRansomRecord(),
			"darkeye_breachhouse_record":     tableBreachHouseRecord(),
		},
	}
}

// getClient returns the connection's client, creating and validating it from the
// connection config the first time. The cache is cleared when the config changes.
func getClient(ctx context.Context, d *plugin.QueryData) (*client, error) {
	if v, ok := d.ConnectionCache.Get(ctx, clientCacheKey); ok {
		if c, ok := v.(*client); ok {
			return c, nil
		}
	}
	o, err := envOptions(GetConfig(d.Connection))
	if err != nil {
		return nil, fmt.Errorf("invalid darkeye connection configuration: %w", err)
	}
	c := newClient(o)
	_ = d.ConnectionCache.Set(ctx, clientCacheKey, c)
	return c, nil
}

// recordColumns are the columns shared by both tables.
func recordColumns() []*plugin.Column {
	return []*plugin.Column{
		{Name: "domain", Type: proto.ColumnType_STRING, Description: "Domain supplied in the query (required equality condition).", Transform: transform.FromQual("domain")},
		{Name: "product", Type: proto.ColumnType_STRING, Description: "Product that returned the record."},
		{Name: "category", Type: proto.ColumnType_STRING, Description: "Record category: ransomware, breaches, leads, stealers or leaks."},
		{Name: "id", Type: proto.ColumnType_STRING, Description: "Identifier of the record in the product."},
		{Name: "title", Type: proto.ColumnType_STRING, Description: "Title of the listing."},
		{Name: "website", Type: proto.ColumnType_STRING, Description: "Website named by the listing."},
		{Name: "group_name", Type: proto.ColumnType_STRING, Description: "Name of the group that made the listing."},
		{Name: "country", Type: proto.ColumnType_STRING, Description: "Country code of the listing."},
		{Name: "discovered", Type: proto.ColumnType_TIMESTAMP, Description: "When the listing was discovered (UTC)."},
	}
}

// streamer sends rows to Steampipe, tells the pagination loop to stop once the
// query needs no more rows, and marks a failure after rows were streamed as not
// retryable (a retry of the whole list call would repeat them).
type streamer struct {
	send     func(Record)
	more     func() bool
	streamed int
}

func newStreamer(ctx context.Context, d *plugin.QueryData) *streamer {
	return &streamer{
		send: func(r Record) { d.StreamListItem(ctx, r) },
		more: func() bool { return d.RowsRemaining(ctx) > 0 },
	}
}

func (s *streamer) emit(r Record) bool {
	s.send(r)
	s.streamed++
	return s.more()
}

func (s *streamer) wrap(err error) error {
	if err != nil && s.streamed > 0 {
		return noRetryError{err}
	}
	return err
}
