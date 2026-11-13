# DarkEye Plugin for Steampipe

Use SQL to query ransomware, breach, lead and stealer listings for a domain from [DarkEye](https://darkeye.io)'s [Have I Been Ransomed?](https://haveibeenransom.com/) and [Breach House](https://breach.house/).

- **[Get started →](docs/index.md)**
- Documentation: [Table definitions & examples](docs/tables)

## Quick start

Install the plugin with [Steampipe](https://steampipe.io):

```shell
steampipe plugin install juanmill4/darkeye
```

Configure your API keys in `~/.steampipe/config/darkeye.spc`, or set `HIBR_API_KEY` and `BREACHHOUSE_API_KEY`. Each table only needs the key of its product; see [docs/index.md](docs/index.md) for where to get each key and the plan requirements.

Run a query:

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

Listings are claims indexed from public sources. They are starting points for investigation and do not confirm an incident; an empty result does not show that a domain is safe.

## Tables

| Table | Product | Key |
| --- | --- | --- |
| [darkeye_haveibeenransom_record](docs/tables/darkeye_haveibeenransom_record.md) | Have I Been Ransomed? | `haveibeenransom_api_key` / `HIBR_API_KEY` |
| [darkeye_breachhouse_record](docs/tables/darkeye_breachhouse_record.md) | Breach House | `breachhouse_api_key` / `BREACHHOUSE_API_KEY` |

Both tables return one row per record and require a `domain` equality condition.

## Developing

Prerequisites:

- [Steampipe](https://steampipe.io/downloads)
- [Golang](https://golang.org/doc/install)

Clone:

```sh
git clone https://github.com/juanmill4/steampipe-plugin-darkeye.git
cd steampipe-plugin-darkeye
```

Build, which automatically installs the new version to your `~/.steampipe/plugins` directory:

```sh
make install
```

Configure the plugin:

```sh
cp config/* ~/.steampipe/config
vi ~/.steampipe/config/darkeye.spc
```

Try it!

```shell
steampipe query
> .inspect darkeye
```

`make test` runs the offline tests (HTTP fixtures, no credentials or live API calls) and `go vet`.

## License

Apache-2.0. See [LICENSE](LICENSE).
