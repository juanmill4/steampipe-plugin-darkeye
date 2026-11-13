---
title: "Steampipe Table: darkeye_haveibeenransom_record - Query Have I Been Ransomed? listings for a domain using SQL"
description: "Allows users to query Have I Been Ransomed? listings for a domain, one row per record, with category, group, country and discovery date."
---

# Table: darkeye_haveibeenransom_record - Query Have I Been Ransomed? listings for a domain using SQL

[Have I Been Ransomed?](https://haveibeenransom.com/) is a DarkEye product that indexes ransomware leak listings and data breaches. Its metadata API returns the listings that mention a domain.

## Table Usage Guide

The `darkeye_haveibeenransom_record` table returns one row per Have I Been Ransomed? listing for a domain. Security analysts can use it to review ransomware and breach claims about their own organization, a supplier or a customer, filter them by category, group or date, and join them with other tables.

The table follows the API's pagination, one request per page, until the API reports no more results. It stops early once a SQL `limit` is reached, and the optional `max_pages` connection argument caps the pages per query. Queries need `haveibeenransom_api_key` (or `HIBR_API_KEY`).

**Important Notes**
- You must specify the `domain` in the `where` clause (`domain = 'example.com'`). URLs, email addresses, IP addresses and wildcards are rejected; internationalized domain names are converted to ASCII.
- `category` is one of `ransomware`, `breaches` or `leaks`.
- Listings are claims indexed from public sources. They are starting points for investigation and do not confirm an incident. An empty result does not show that a domain is safe.
- The plugin returns listing metadata only. It does not request leaked records or download files.

## Examples

### Basic info
List every listing for a domain, newest first.

```sql+postgres
select
  category,
  title,
  group_name,
  country,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
order by
  discovered desc;
```

```sql+sqlite
select
  category,
  title,
  group_name,
  country,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
order by
  discovered desc;
```

### List ransomware listings only
Show only the claims made by ransomware groups.

```sql+postgres
select
  title,
  group_name,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
  and category = 'ransomware';
```

```sql+sqlite
select
  title,
  group_name,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
  and category = 'ransomware';
```

### List listings from the last 90 days
Find recent claims to prioritize investigation.

```sql+postgres
select
  category,
  title,
  group_name,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
  and discovered > now() - interval '90 days';
```

```sql+sqlite
select
  category,
  title,
  group_name,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
  and discovered > datetime('now', '-90 days');
```

### Count listings by category
Get a quick summary of a domain's exposure.

```sql+postgres
select
  category,
  count(*) as listings
from
  darkeye_haveibeenransom_record
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
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
group by
  category
order by
  listings desc;
```

### Get the five most recent listings
A SQL `limit` also stops the plugin from requesting further pages.

```sql+postgres
select
  category,
  title,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
order by
  discovered desc
limit 5;
```

```sql+sqlite
select
  category,
  title,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com'
order by
  discovered desc
limit 5;
```
