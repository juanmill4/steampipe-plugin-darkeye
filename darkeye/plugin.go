package darkeye

import (
	"context"
	"os"

	"github.com/turbot/steampipe-plugin-sdk/v5/grpc/proto"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin/transform"
)

type Config struct {
	HIBRKey  *string `hcl:"haveibeenransom_api_key,optional"`
	BHKey    *string `hcl:"breachhouse_api_key,optional"`
	MaxPages *int    `hcl:"max_pages,optional"`
	Limit    *int    `hcl:"limit,optional"`
}

func (c Config) options() Options {
	o := Options{HIBRKey: os.Getenv("HIBR_API_KEY"), BHKey: os.Getenv("BREACHHOUSE_API_KEY"), MaxPages: 5, Limit: 25}
	if c.HIBRKey != nil {
		o.HIBRKey = *c.HIBRKey
	}
	if c.BHKey != nil {
		o.BHKey = *c.BHKey
	}
	if c.MaxPages != nil {
		o.MaxPages = *c.MaxPages
	}
	if c.Limit != nil {
		o.Limit = *c.Limit
	}
	return o
}

func Plugin(_ context.Context) *plugin.Plugin {
	return &plugin.Plugin{
		Name:                   "steampipe-plugin-darkeye",
		DefaultTransform:       transform.FromJSONTag(),
		ConnectionConfigSchema: &plugin.ConnectionConfigSchema{NewInstance: func() interface{} { return &Config{} }},
		TableMap: map[string]*plugin.Table{"darkeye_domain": {
			Name:        "darkeye_domain",
			Description: "Domain exposure metadata from DarkEye products. Listings do not establish a confirmed incident.",
			List:        &plugin.ListConfig{Hydrate: listDomain, KeyColumns: plugin.KeyColumnSlice{{Name: "domain", Require: plugin.Required}}},
			Columns: []*plugin.Column{
				{Name: "domain", Type: proto.ColumnType_STRING, Description: "Domain supplied in the query.", Transform: transform.FromQual("domain")},
				{Name: "provider", Type: proto.ColumnType_STRING, Description: "Provider: DarkEye (darkeye.io)."},
				{Name: "records", Type: proto.ColumnType_JSON, Description: "Metadata records, each with its product and category."},
				{Name: "products", Type: proto.ColumnType_JSON, Description: "Products queried using configured keys."},
				{Name: "errors", Type: proto.ColumnType_JSON, Description: "Product errors without response bodies or credentials."},
				{Name: "truncated", Type: proto.ColumnType_BOOL, Description: "A request limit may have omitted records."},
				{Name: "complete", Type: proto.ColumnType_BOOL, Description: "All configured requests completed without known truncation. This does not establish absence of exposure."},
			},
		}},
	}
}

func listDomain(ctx context.Context, d *plugin.QueryData, _ *plugin.HydrateData) (interface{}, error) {
	c, _ := d.Connection.Config.(Config)
	r, err := newClient().lookup(ctx, d.EqualsQuals["domain"].GetStringValue(), c.options())
	if err != nil {
		return nil, err
	}
	d.StreamListItem(ctx, r)
	return nil, nil
}
