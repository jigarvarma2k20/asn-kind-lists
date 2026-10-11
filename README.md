# ASN Kind Lists

Downloads the full list of Autonomous System Numbers (ASNs), classifies each one
as **hosting**, **residential**, or **unknown**, and commits the results as plain
text lists under [`data/`](data).

## How it works

1. Downloads the ASN list from
   [`ipverse/as-metadata`](https://github.com/ipverse/as-metadata) (`as.csv`).
2. For every ASN, queries the ipinfo ASN details API:
   `https://asn.ipinfo.app/api/json/details/AS<ASN>`
   and reads the `kind` and `name` fields.
3. Writes one file per `kind` into `data/`, sorted by ASN.

Requests run concurrently across 500 workers with automatic retries for
transient HTTP errors (429/5xx) before the classification pass.

## Data files

| File | Description |
| --- | --- |
| [`data/hosting.txt`](data/hosting.txt) | Datacenter / cloud provider ASNs (`kind` = `hosting`). |
| [`data/residential.txt`](data/residential.txt) | ISP ASNs (`kind` = `residential`). |
| [`data/unknown.txt`](data/unknown.txt) | ASNs whose `kind` is `null`/missing (mapped to `unknown`). |
| [`data/failed.txt`](data/failed.txt) | ASNs that could not be classified (see below). |

### Line format

Each entry is:

```
<ASN> # <name>
```

Example:

```
249 # PACKETSTAR European Network
```

### Classification rules

- `kind` = `hosting` → `hosting.txt` (datacenter / cloud providers)
- `kind` = `residential` → `residential.txt` (ISPs)
- `kind` = `null` → `unknown.txt`
- any other value → failed

## Failures and retries

- **HTTP 404** — the ASN does not exist in the API. Not retried; written to
  `failed.txt`.
- **Non-JSON / unparsable response** — queued for a retry. After the first pass,
  all queued ASNs are retried once.
- **Still unparsable after the retry** — written to `failed.txt`.

`data/failed.txt` contains a single ASN per line.

## Running locally

Requires Go 1.25+.

```sh
go run .
```

The lists are written to `data/`.

## Automation

The GitHub Actions workflow
[`.github/workflows/update-asn.yml`](.github/workflows/update-asn.yml) rebuilds
and commits the lists:

- **Scheduled** — every 3 days at 03:00 UTC.
- **Manual** — via *Actions → Update ASN Lists → Run workflow*.

## Credits

- ASN source list: [`ipverse/as-metadata`](https://github.com/ipverse/as-metadata).
- Classification data: [ipinfo](https://asn.ipinfo.app) ASN details API.

## License

Licensed under the [GNU General Public License v3.0](LICENSE). The bundled data
is derived from the third-party sources credited above and is subject to their
respective terms.
