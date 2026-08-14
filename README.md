# account-info

An HTTP profile service for [AT Protocol](https://atproto.com/) accounts.

## Account profiles

Request an account by handle or DID to receive its profile as JSON:

```console
curl https://account.info/calabro.io
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
