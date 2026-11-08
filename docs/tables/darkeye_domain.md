# Table: darkeye_domain

Returns a DarkEye metadata report for a domain using the configured Have I Been Ransomed? and Breach House API keys. Supply `domain` with an equality condition. At least one product key is required.

| Column | Type | Meaning |
| --- | --- | --- |
| domain | text | Domain supplied in the query. |
| provider | text | DarkEye. |
| products | jsonb | Products queried. |
| records | jsonb | Metadata records, each with product and category. |
| errors | jsonb | Product and sanitized error message for failed requests. |
| truncated | boolean | A request limit may have omitted records. |
| complete | boolean | All configured requests completed without known truncation. |

The `records` array contains product, category, ID, title, website, group, country and discovery date. Categories retain the distinctions made by the source: ransomware, breaches, leaks, leads or stealers. Unknown HIBR types are reported as leaks. No leaked record content or download links are included.

## Inspect completeness before interpreting results

```sql
select domain, products, errors, truncated, complete
from darkeye_domain
where domain = 'example.com';
```

## Expand metadata records

```sql
select
  record ->> 'product' as product,
  record ->> 'category' as category,
  record ->> 'title' as title,
  record ->> 'discovered' as discovered
from darkeye_domain,
  jsonb_array_elements(records) as record
where domain = 'example.com';
```

An empty `records` array with errors is a failed or partial lookup. Even a complete empty response does not establish absence of exposure. Indexed listings require investigation and do not establish a confirmed incident.
