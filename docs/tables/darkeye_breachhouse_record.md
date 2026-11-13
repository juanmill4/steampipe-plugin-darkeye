---
title: "Steampipe Table: darkeye_breachhouse_record - Query Breach House listings for a domain using SQL"
description: "Allows users to query Breach House listings for a domain, one row per record, with category, group, country and discovery date."
---

# Table: darkeye_breachhouse_record - Query Breach House listings for a domain using SQL

[Breach House](https://breach.house/) is a DarkEye product that searches ransomware listings, data breaches, access leads and stealer logs. Its search API returns the matching listings from each source.

## Table Usage Guide

The `darkeye_breachhouse_record` table returns one row per Breach House listing whose website, or a hostname in its title, matches the domain or one of its subdomains. Security analysts can use it to see ransomware claims, breaches, access leads and stealer log listings for a domain in one query, and to compare them by category or date.

The table makes one search request per query and asks for up to 200 results per source, or the SQL `limit` if it is smaller. The search endpoint does not paginate; when a source has more results than were returned, the plugin logs a warning. Queries need `breachhouse_api_key` (or `BREACHHOUSE_API_KEY`) and a Breach House plan with API access (advanced or enterprise).

**Important Notes**
- You must specify the `domain` in the `where` clause (`domain = 'example.com'`). URLs, email addresses, IP addresses and wildcards are rejected; internationalized domain names are converted to ASCII.
- `category` is one of `ransomware`, `breaches`, `leads` or `stealers`.
- Listings are claims indexed from public sources. They are starting points for investigation and do not confirm an incident. An empty result does not show that a domain is safe.
- The plugin returns listing metadata only. It does not request leaked records or download files.

## Examples

### Basic info
List every matching listing for a domain, newest first.

```sql+postgres
select
  category,
  title,
  website,
  group_name,
  discovered
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
order by
  discovered desc;
```

```sql+sqlite
select
  category,
  title,
  website,
  group_name,
  discovered
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
order by
  discovered desc;
```

### List stealer log listings
Find stealer log listings that mention the domain.

```sql+postgres
select
  id,
  title,
  website,
  discovered
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
  and category = 'stealers';
```

```sql+sqlite
select
  id,
  title,
  website,
  discovered
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
  and category = 'stealers';
```

### Count listings by category
Get a quick summary across the four Breach House sources.

```sql+postgres
select
  category,
  count(*) as listings
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
group by
  category
order by
  listings desc;
```

```sql+sqlite
select
  category,
  count(*) as listings
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
group by
  category
order by
  listings desc;
```

### List listings from the last 30 days
Find recent listings to prioritize investigation.

```sql+postgres
select
  category,
  title,
  discovered
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
  and discovered > now() - interval '30 days';
```

```sql+sqlite
select
  category,
  title,
  discovered
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
  and discovered > datetime('now', '-30 days');
```

### Compare both products
Combine the listings from both DarkEye tables for one domain (both keys required).

```sql+postgres
select
  product,
  category,
  title,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
union all
select
  product,
  category,
  title,
  discovered
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
order by
  discovered desc;
```

```sql+sqlite
select
  product,
  category,
  title,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
union all
select
  product,
  category,
  title,
  discovered
from
  darkeye_breachhouse_record
where
  domain = 'example.com'
order by
  discovered desc;
```
