---
organization: juanmill4
category: ["security"]
icon_url: "/images/plugins/juanmill4/darkeye.svg"
brand_color: "#00F2FF"
display_name: "DarkEye"
short_name: "darkeye"
description: "Steampipe plugin to query ransomware, breach, lead and stealer listings for a domain from DarkEye's Have I Been Ransomed? and Breach House."
og_description: "Query DarkEye domain exposure listings with SQL! Open source CLI. No DB required."
og_image: "/images/plugins/juanmill4/darkeye-social-graphic.png"
engines: ["steampipe", "sqlite", "postgres", "export"]
---

# DarkEye + Steampipe

[Steampipe](https://steampipe.io) is an open-source zero-ETL engine to instantly query cloud APIs using SQL.

[DarkEye](https://darkeye.io) builds threat intelligence products. This plugin covers two of them: [Have I Been Ransomed?](https://haveibeenransom.com/), which indexes ransomware leak listings and data breaches, and [Breach House](https://breach.house/), which searches ransomware listings, breaches, access leads and stealer logs. Each product has its own table and its own optional API key.

For example:

```sql
select
  category,
  title,
  group_name,
  discovered
from
  darkeye_haveibeenransom_record
where
  domain = 'example.com';
```

```
+------------+-------------+------------+---------------------------+
| category   | title       | group_name | discovered                |
+------------+-------------+------------+---------------------------+
| ransomware | example.com | examplegrp | 2024-12-16T21:07:57+00:00 |
| breaches   | Example Inc |            | 2023-05-02T10:14:03+00:00 |
+------------+-------------+------------+---------------------------+
```

Listings are claims indexed from public sources. They are starting points for investigation and do not confirm an incident; an empty result does not show that a domain is safe.

## Documentation

- **[Table definitions & examples →](/plugins/juanmill4/darkeye/tables)**

## Get started

### Install

Download and install the latest DarkEye plugin:

```bash
steampipe plugin install juanmill4/darkeye
```

### Credentials

| Item        | Description |
| ----------- | ----------- |
| Credentials | Each product uses its own API token. Have I Been Ransomed?: every plan includes a token, which you copy from your profile ([API docs](https://haveibeenransom.com/howapi)). Breach House: the API needs an advanced or enterprise plan; the token is in your customer panel ([API docs](https://breach.house/docs)). |
| Permissions | A table only needs the key of its product. Without that key, queries to the table return an error; the other table keeps working. |
| Radius      | Each connection represents one set of DarkEye product tokens. |
| Resolution  | 1. Credentials explicitly set in a steampipe config file (`~/.steampipe/config/darkeye.spc`).<br />2. Credentials specified in the `HIBR_API_KEY` and `BREACHHOUSE_API_KEY` environment variables. |

### Configuration

Installing the latest darkeye plugin will create a config file (`~/.steampipe/config/darkeye.spc`) with a single connection named `darkeye`:

```hcl
connection "darkeye" {
  plugin = "juanmill4/darkeye"

  # Have I Been Ransomed? API token. Every plan includes one; copy it from your
  # profile. See https://haveibeenransom.com/howapi
  # Can also be set with the HIBR_API_KEY environment variable.
  #haveibeenransom_api_key = "q8Zr3mT0vXk7LwN2pYc5HdJ9sBf4GuAe6RiK1oVnMx"

  # Breach House API token. The API needs an advanced or enterprise plan; the
  # token is in your customer panel. See https://breach.house/docs
  # Can also be set with the BREACHHOUSE_API_KEY environment variable.
  #breachhouse_api_key = "Wd4nT7yQm2Lk9XzR5vBc8HsJ3pGf6NaE1uKoYiZtMw"

  # Maximum number of Have I Been Ransomed? pages requested per query. Each page
  # is one request against your plan's daily budget. If not set, the plugin
  # follows pagination until the API reports no more results.
  #max_pages = 5
}
```

Alternatively, you can use the product environment variables:

```sh
export HIBR_API_KEY=q8Zr3mT0vXk7LwN2pYc5HdJ9sBf4GuAe6RiK1oVnMx
export BREACHHOUSE_API_KEY=Wd4nT7yQm2Lk9XzR5vBc8HsJ3pGf6NaE1uKoYiZtMw
```

A key set in the connection config, even an empty one, overrides its environment variable.

### Request budgets

Both products limit requests per day by plan. Steampipe stops paging as soon as a SQL `limit` is satisfied, so adding a `limit` saves requests.

- `darkeye_haveibeenransom_record` makes one request per page of results. Use `max_pages` to cap the pages per query.
- `darkeye_breachhouse_record` makes one search request per query, asking for up to 200 results per source (or the SQL `limit`, if it is smaller).
- When a budget is used up, the API returns HTTP 429 and the query fails with an error that says so. A 429 is not retried; server errors (5xx) and transient network errors are.

## Multiple Connections

You may create multiple darkeye connections, for example one per team or per set of product tokens:

```hcl
connection "darkeye_team_a" {
  plugin                  = "juanmill4/darkeye"
  haveibeenransom_api_key = "q8Zr3mT0vXk7LwN2pYc5HdJ9sBf4GuAe6RiK1oVnMx"
}

connection "darkeye_team_b" {
  plugin              = "juanmill4/darkeye"
  breachhouse_api_key = "Wd4nT7yQm2Lk9XzR5vBc8HsJ3pGf6NaE1uKoYiZtMw"
}
```

Each connection is implemented as a distinct [Postgres schema](https://www.postgresql.org/docs/current/ddl-schemas.html). You can also create an [aggregator](https://steampipe.io/docs/managing/connections#using-aggregators) connection to query several of them as one.
