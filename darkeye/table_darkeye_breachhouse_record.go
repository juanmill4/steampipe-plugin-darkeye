package darkeye

import (
	"context"
	"fmt"

	"github.com/turbot/steampipe-plugin-sdk/v6/plugin"
)

func tableBreachHouseRecord() *plugin.Table {
	return &plugin.Table{
		Name:        "darkeye_breachhouse_record",
		Description: "Metadata listings for a domain from Breach House, one row per record. Listings are claims and do not confirm an incident.",
		List: &plugin.ListConfig{
			Hydrate:    listBreachHouseRecords,
			KeyColumns: plugin.SingleColumn("domain"),
		},
		Columns: recordColumns(),
	}
}

func listBreachHouseRecords(ctx context.Context, d *plugin.QueryData, _ *plugin.HydrateData) (interface{}, error) {
	logger := plugin.Logger(ctx)
	domain := d.EqualsQualString("domain")

	c, err := getClient(ctx, d)
	if err != nil {
		logger.Error("darkeye_breachhouse_record.list", "connection_error", err)
		return nil, err
	}
	normalized, err := normalizeDomain(domain)
	if err != nil {
		logger.Error("darkeye_breachhouse_record.list", "domain_error", err, "domain", domain)
		return nil, fmt.Errorf("invalid domain %q: %w", domain, err)
	}
	if d.RowsRemaining(ctx) == 0 {
		return nil, nil
	}

	s := newStreamer(ctx, d)
	limit := requestLimit(d.QueryContext.Limit)
	omitted, err := c.streamBH(ctx, normalized, limit, s.emit)
	if err != nil {
		logger.Error("darkeye_breachhouse_record.list", "api_error", err, "domain", domain)
		return nil, s.wrap(fmt.Errorf("darkeye_breachhouse_record: lookup for %q failed: %w", domain, err))
	}
	if omitted {
		logger.Warn("darkeye_breachhouse_record.list", "results_beyond_limit", limit, "domain", domain)
	}
	return nil, nil
}
