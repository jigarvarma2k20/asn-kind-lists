# ASN Kind Lists

Downloads the full list of Autonomous System Numbers (ASNs), groups each one by
its `category`, and commits the results as plain text lists under
[`data/`](data).

## How it works

1. Downloads the ASN metadata from
   [`ipverse/as-metadata`](https://github.com/ipverse/as-metadata) (`as.json`).
2. For every ASN, reads the `metadata.category` and `metadata.handle` fields.
3. Writes one file per `category` into `data/`, sorted by ASN.
4. Downloads the [InternetData](https://internetdata.io) `asn_v1` database and
   keeps only the ASNs whose network `type` is `hosting` in `data/hosting.txt`.

## Data files

One file is written per `category`, named after the lowercased category. The
current categories are:

| File | Description |
| --- | --- |
| [`data/hosting.txt`](data/hosting.txt) | Datacenter / cloud providers, confirmed as `hosting` by InternetData. |
| [`data/isp.txt`](data/isp.txt) | Internet service providers. |
| [`data/business.txt`](data/business.txt) | Business / enterprise networks. |
| [`data/education_research.txt`](data/education_research.txt) | Education and research networks. |
| [`data/government_admin.txt`](data/government_admin.txt) | Government and administrative networks. |
| [`data/unknown.txt`](data/unknown.txt) | ASNs with no `category`. |

### Line format

Each entry is:

```
<ASN> # <handle>
```

Example:

```
1 # LVLT-1
```

If `handle` is empty, the `description` is used instead.

### Category mapping

- `category` present → `data/<category>.txt`
- `category` empty → `data/unknown.txt`

### Hosting filter

`data/hosting.txt` is verified against the
[InternetData `asn_v1` database](https://internetdata.io/databases/asn), which
classifies every allocated ASN as `isp`, `hosting`, `education`, `government` or
`business`. Only ASNs the database reports as `hosting` are kept.

This requires an API key in the `INTERNETDATA_APIKEY` environment variable
(free with an InternetData account).

## Running locally

Requires Go 1.25+.

```sh
INTERNETDATA_APIKEY=your_key go run .
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

## License

Licensed under the [GNU General Public License v3.0](LICENSE). The bundled data
is derived from the third-party sources credited above and is subject to their
respective terms.
