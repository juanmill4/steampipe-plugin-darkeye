package darkeye

import (
	"context"
	"fmt"

	"github.com/turbot/steampipe-plugin-sdk/v6/plugin"
)

func tableHaveIBeenRansomRecord() *plugin.Table {
	return &plugin.Table{
		Name:        "darkeye_haveibeenransom_record",
		Description: "Metadata listings for a domain from Have I Been Ransomed?, one row per record. Listings are claims and do not confirm an incident.",
		List: &plugin.ListConfig{
			Hydrate:    listHaveIBeenRansomRecords,
			KeyColumns: plugin.SingleColumn("domain"),
		},
		Columns: recordColumns(),
	}
}

func listHaveIBeenRansomRecords(ctx context.Context, d *plugin.QueryData, _ *plugin.HydrateData) (interface{}, error) {
	logger := plugin.Logger(ctx)
	domain := d.EqualsQualString("domain")

	c, err := getClient(ctx, d)
	if err != nil {
		logger.Error("darkeye_haveibeenransom_record.list", "connection_error", err)
		return nil, err
	}
	normalized, err := normalizeDomain(domain)
	if err != nil {
		logger.Error("darkeye_haveibeenransom_record.list", "domain_error", err, "domain", domain)
		return nil, fmt.Errorf("invalid domain %q: %w", domain, err)
	}
	if d.RowsRemaining(ctx) == 0 {
		return nil, nil
	}

	s := newStreamer(ctx, d)
	capped, err := c.streamHIBR(ctx, normalized, s.emit)
	if err != nil {
		logger.Error("darkeye_haveibeenransom_record.list", "api_error", err, "domain", domain)
		return nil, s.wrap(fmt.Errorf("darkeye_haveibeenransom_record: lookup for %q failed: %w", domain, err))
	}
	if capped {
		logger.Warn("darkeye_haveibeenransom_record.list", "max_pages_reached", c.maxPages, "domain", domain)
	}
	return nil, nil
}
