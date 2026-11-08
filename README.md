# DarkEye plugin for Steampipe

Query domain exposure metadata from [DarkEye](https://darkeye.io) with SQL. One plugin covers [Have I Been Ransomed?](https://haveibeenransom.com/) and [Breach House](https://breach.house/), with a separate optional API key for each product. Configure at least one key.

The `darkeye_domain` table returns one report per domain. Each record identifies its product and category. The report includes errors and truncation status, even when no records are returned. Listings are research leads, not proof of a confirmed incident; an empty report does not establish that a domain is safe.

## Build and install locally

Requires Go 1.26 or later and Steampipe. This repository provides a source build; it is not yet distributed through the Steampipe Hub.

```sh
git clone https://github.com/juanmill4/steampipe-plugin-darkeye.git
cd steampipe-plugin-darkeye
make test
make install
mkdir -p ~/.steampipe/config
cp config/darkeye.spc ~/.steampipe/config/darkeye.spc
```

Set `HIBR_API_KEY`, `BREACHHOUSE_API_KEY`, or both in the environment used to start Steampipe. Alternatively, add `haveibeenransom_api_key` or `breachhouse_api_key` to the connection configuration. A configured value overrides its environment variable; an empty value disables that product. Keep keys out of version control. API access depends on your account with each product; this plugin does not provide credentials.

Restart an existing Steampipe service after installing the plugin or changing its environment.

```sql
select domain, provider, records, errors, truncated, complete
from darkeye_domain
where domain = 'example.com';
```

## Query behavior

- A domain equality filter is required. Wildcards, URLs, email addresses and IP addresses are rejected. Internationalized domain names are converted to ASCII.
- Have I Been Ransomed? uses `/api/metadata/domain/{domain}` and follows `has_next`, including empty intermediate pages. `max_pages` defaults to 5 and accepts 1–50. Each page makes one API request.
- Breach House uses `/api/v1/search` once, with `q` and `limit`. `limit` defaults to 25 and accepts 1–200 per category. Results are retained only when their website hostname or a hostname in their title matches the queried domain or a subdomain. This filter can omit relevant listings that do not identify the domain in those fields.
- These limits are plugin settings, not guarantees about product quotas. `truncated` flags a page cap, a reported total larger than the returned set, or a category reaching its limit without a total.
- Configured products are queried independently. Successful records and earlier pages remain available if another request fails. Check `errors` and `truncated`; `complete` is true only when configured requests finish without known truncation.
- Output is limited to listing metadata: product, category, ID, title, website, group, country and discovery date. The plugin does not request leaked records or download files. HTTP redirects are refused. Each request has a 30-second timeout and a 4 MiB response limit; errors omit response bodies and credentials.
- Querying sends the supplied domain to the configured products. Steampipe may cache query results according to its configuration. Use domains you are authorized to investigate.

See [table documentation](docs/tables/darkeye_domain.md) for examples and fields.

## Development

`make test` runs HTTP fixture tests and Go vet without credentials or external API calls. The fixtures cover pagination, optional keys, partial failures, malformed responses, domain matching, redirects, response limits and cancellation. These tests do not verify the providers' live availability, account permissions or quotas.

## License

Apache-2.0. See [LICENSE](LICENSE).
