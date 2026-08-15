# account-info

An HTTP profile service for [AT Protocol](https://atproto.com/) accounts.

## Account profiles

Open an account by handle or DID in a browser to see its default profile and
every other supported profile record on a small landing page:

```text
https://account.info/calabro.io
```

API clients receive the default profile and every other supported profile
record as JSON:

```console
curl https://account.info/calabro.io
```

The account endpoint negotiates between HTML, JSON, and image responses using
the `Accept` header. Explicit preferences take priority. If the header is
missing or ambiguous, browser user agents receive HTML while common API clients
such as curl, Go, Python, and Node receive JSON. Callers can always select a
representation explicitly:

```console
curl -H 'Accept: application/json' https://account.info/calabro.io
curl -H 'Accept: text/html' https://account.info/calabro.io
```

## Avatars

The canonical avatar endpoint serves the profile's original JPEG or PNG:

```console
curl -o avatar https://account.info/avatar/calabro.io
```

The account endpoint also negotiates an image representation. It redirects to
the canonical avatar endpoint so CDNs store the image under one URL instead of
fragmenting the image cache by the exact `Accept` header:

```console
curl -L -H 'Accept: image/*' https://account.info/calabro.io -o avatar
```

Avatar responses include a strong ETag derived from the blob CID and are
publicly cacheable for five minutes. Only the AT Protocol profile lexicon's
JPEG and PNG types are served, with its 1 MB size limit enforced.

## Account lookup cache

Successful account lookups are cached in memory for five minutes. Failed
lookups are cached for 30 seconds, except for caller cancellation and timeout
errors. The cache uses least-recently-used eviction and stores at most one
million entries across successful and failed lookups.

The defaults can be tuned with command-line flags or environment variables:

| Flag | Environment variable | Default |
| --- | --- | --- |
| `--cache-ttl` | `ACCOUNT_INFO_CACHE_TTL` | `5m` |
| `--cache-error-ttl` | `ACCOUNT_INFO_CACHE_ERROR_TTL` | `30s` |
| `--cache-max-entries` | `ACCOUNT_INFO_CACHE_MAX_ENTRIES` | `1000000` |

## Lookup rate limit

Account and avatar requests are limited by source IP using a token-bucket-style
limit. The default allows three requests per second with a burst of three;
capacity then refills at one request every third of a second. Health checks,
static assets, the home page, and lookup-form redirects do not consume the
limit. Rejected requests return `429 Too Many Requests` with `Retry-After` and
a JSON error response.

The limit can be changed or disabled with a command-line flag or environment
variable:

| Flag | Environment variable | Default |
| --- | --- | --- |
| `--lookup-rate-limit` | `ACCOUNT_INFO_LOOKUP_RATE_LIMIT` | `3` |

A value of `0` disables rate limiting. Negative values fail startup.

The limiter uses the TCP peer address and intentionally ignores forwarding
headers, which are spoofable without an explicit trusted-proxy policy. If the
service runs behind a reverse proxy, enforce the equivalent limit there or
ensure the application receives the original client as its peer. The limiter
is local to each process, so multiple replicas do not share budgets. Its source
table is bounded to prevent untrusted IP churn from causing unbounded memory
growth; if every tracked source is still active at capacity, new sources are
rejected until an entry has fully replenished.
