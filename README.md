# account-info

[account.info](https://account.info) is a HTTPS profile service for [AT Protocol](https://atproto.com/) accounts.

## Account profiles

Open an account by handle or DID in a browser to see its public profile on a
small landing page:

```text
https://account.info/bsky.app
```

API clients receive the default profile and every other supported profile
record as JSON:

```console
curl https://account.info/bsky.app
```

The account endpoint negotiates between HTML, JSON, and image responses using
the `Accept` header. Explicit preferences take priority. If the header is
missing or ambiguous, browsers and common link-preview crawlers receive HTML
while API clients such as curl, Go, Python, and Node receive JSON. Callers can
always select a representation explicitly:

```console
curl -H 'Accept: application/json' https://account.info/bsky.app
curl -H 'Accept: text/html' https://account.info/bsky.app
```

## Supported profile records

Supported record types are declared in [`profiles.json`](profiles.json). Each
entry identifies the collection and record key to fetch from the account's PDS
and provides singular [RFC 9535 JSONPath](https://www.rfc-editor.org/rfc/rfc9535.html)
expressions for `displayName`, `description`, `avatar`, and `createdAt`.
Selected values populate the top-level account summary; the complete record is
always returned under `profiles`.

Missing or malformed selected fields are treated as unavailable without
discarding the complete profile record. With multiple records, the default is
the oldest profile with a valid `createdAt`; if none has a usable timestamp,
`default` and the derived top-level profile fields are omitted.

An entry may also define application-link metadata. Its `profileURL` must be an
HTTPS URL containing one `{identifier}` placeholder in the path. Its `iconURL`
must be an HTTPS URL whose path ends in `.svg`, `.png`, `.jpg`, or `.jpeg`.
Discovered application links appear as an icon row on the public profile page.

## Avatars

The canonical avatar endpoint serves the profile's original JPEG, PNG, or WebP:

```console
curl -o avatar https://account.info/avatar/bsky.app
```

The account endpoint also negotiates an image representation. It redirects to
the canonical avatar endpoint so CDNs store the image under one URL instead of
fragmenting the image cache by the exact `Accept` header:

```console
curl -L -H 'Accept: image/*' https://account.info/bsky.app -o avatar
```

Avatar responses include a strong ETag derived from the blob CID and are
publicly cacheable for five minutes. JPEG, PNG, and WebP images are served,
with the AT Protocol profile lexicon's 1 MB size limit enforced.

## Link previews

Account pages expose Open Graph and Twitter Card metadata. Services such as
Slack can use the profile's display name, handle, description, and avatar when
expanding an account URL. Preview image and canonical URLs use the fixed public
`https://account.info` origin rather than the request's untrusted `Host` header.
