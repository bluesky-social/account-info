package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jcalabro/atmos"
	"github.com/jcalabro/atmos/api/bsky"
	"github.com/jcalabro/atmos/api/comatproto"
	"github.com/jcalabro/atmos/cbor"
	"github.com/jcalabro/atmos/identity"
	"github.com/jcalabro/atmos/xrpc"
	"github.com/jcalabro/gt"
)

const profileRecordKey = "self"

const maxAvatarSize int64 = 1_000_000

type atprotoResolver struct {
	directory *identity.Directory
}

func (r *atprotoResolver) Resolve(
	ctx context.Context,
	raw string,
) (Identity, error) {
	identifier, err := atmos.ParseATIdentifier(raw)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidIdentifier, err)
	}

	resolved, err := r.directory.Lookup(ctx, identifier.Normalize())
	if err != nil {
		if errors.Is(err, identity.ErrDIDNotFound) ||
			errors.Is(err, identity.ErrHandleNotFound) {
			return Identity{}, fmt.Errorf("%w: %s", ErrIdentityNotFound, raw)
		}
		return Identity{}, fmt.Errorf("resolve identity: %w", err)
	}

	result := Identity{
		DID: string(resolved.DID),
		PDS: resolved.PDSEndpoint(),
	}
	if resolved.Handle != atmos.HandleInvalid {
		result.Handle = string(resolved.Handle)
	}
	return result, nil
}

type atprotoRecordReader struct {
	httpClient *http.Client
}

func (r *atprotoRecordReader) Get(
	ctx context.Context,
	account Identity,
	source Source,
) (Record, error) {
	if err := validatePDSURL(account.PDS); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrNoPDS, err)
	}
	client := &xrpc.Client{
		Host:       account.PDS,
		HTTPClient: gt.Some(r.httpClient),
	}
	output, err := comatproto.RepoGetRecord(
		ctx,
		client,
		"",
		source.Collection,
		account.DID,
		source.RecordKey,
	)
	if err != nil {
		// Only the protocol-defined RecordNotFound error means the record
		// is absent. A bare 404 (missing XRPC route, misconfigured proxy,
		// wrong endpoint) must propagate rather than read as "no profile".
		var xrpcErr *xrpc.Error
		if errors.As(err, &xrpcErr) &&
			xrpcErr.Name == comatproto.ErrRepoGetRecord_RecordNotFound {
			return Record{}, ErrRecordNotFound
		}
		return Record{}, err
	}

	uri, err := atmos.ParseATURI(output.URI)
	if err != nil {
		return Record{}, fmt.Errorf("invalid record URI: %w", err)
	}
	if uri.Authority().String() != account.DID ||
		uri.Collection().String() != source.Collection ||
		uri.RecordKey().String() != source.RecordKey {
		return Record{}, fmt.Errorf("record URI does not match request: %s", uri)
	}

	return Record{
		Collection: source.Collection,
		URI:        output.URI,
		CID:        output.CID.ValOr(""),
		Value:      output.Value,
	}, nil
}

func (r *atprotoRecordReader) GetBlob(
	ctx context.Context,
	account Identity,
	ref BlobRef,
) (Avatar, error) {
	if err := validatePDSURL(account.PDS); err != nil {
		return Avatar{}, fmt.Errorf("%w: %w", ErrNoPDS, err)
	}
	claimedCID, err := validateAvatarRef(ref)
	if err != nil {
		return Avatar{}, err
	}

	location, err := blobURL(account.PDS, account.DID, ref.CID)
	if err != nil {
		return Avatar{}, fmt.Errorf("build blob URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, http.NoBody)
	if err != nil {
		return Avatar{}, fmt.Errorf("build blob request: %w", err)
	}
	request.Header.Set("Accept", "image/png, image/jpeg")

	response, err := r.httpClient.Do(request)
	if err != nil {
		return Avatar{}, fmt.Errorf("fetch blob: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return Avatar{}, fmt.Errorf("fetch blob: unexpected HTTP status %s", response.Status)
	}
	if response.ContentLength > ref.Size {
		return Avatar{}, fmt.Errorf(
			"avatar body exceeds declared size: content-length %d, declared %d",
			response.ContentLength,
			ref.Size,
		)
	}

	content, err := io.ReadAll(io.LimitReader(response.Body, ref.Size+1))
	if err != nil {
		return Avatar{}, fmt.Errorf("read blob: %w", err)
	}
	if int64(len(content)) != ref.Size {
		return Avatar{}, fmt.Errorf(
			"avatar size mismatch: got %d, declared %d",
			len(content),
			ref.Size,
		)
	}
	if detected := http.DetectContentType(content); detected != ref.ContentType {
		return Avatar{}, fmt.Errorf(
			"avatar content type mismatch: detected %s, declared %s",
			detected,
			ref.ContentType,
		)
	}
	actualCID := cbor.ComputeCID(cbor.CodecRaw, content)
	if !bytes.Equal(actualCID.Bytes(), claimedCID.Bytes()) {
		return Avatar{}, fmt.Errorf("avatar content does not match CID %s", ref.CID)
	}

	return Avatar{
		Content:     content,
		ContentType: ref.ContentType,
		CID:         ref.CID,
	}, nil
}

func validateAvatarRef(ref BlobRef) (cbor.CID, error) {
	if ref.ContentType != "image/png" && ref.ContentType != "image/jpeg" {
		return cbor.CID{}, fmt.Errorf("unsupported avatar content type %q", ref.ContentType)
	}
	if ref.Size <= 0 || ref.Size > maxAvatarSize {
		return cbor.CID{}, fmt.Errorf("invalid avatar size %d", ref.Size)
	}
	cid, err := cbor.ParseCIDString(ref.CID)
	if err != nil {
		return cbor.CID{}, fmt.Errorf("invalid avatar CID: %w", err)
	}
	if cid.Codec() != cbor.CodecRaw {
		return cbor.CID{}, fmt.Errorf("avatar CID must use the raw codec")
	}
	return cid, nil
}

func extractBlueskyProfile(
	account Identity,
	value json.RawMessage,
) (Summary, error) {
	var record bsky.ActorProfile
	if err := json.Unmarshal(value, &record); err != nil {
		return Summary{}, err
	}

	summary := Summary{
		DisplayName: record.DisplayName.ValOr(""),
		Description: record.Description.ValOr(""),
	}
	if record.Avatar.HasVal() {
		blob := record.Avatar.Val()
		avatarURL, err := blobURL(
			account.PDS,
			account.DID,
			blob.Ref.Link,
		)
		if err != nil {
			return Summary{}, fmt.Errorf("build avatar URL: %w", err)
		}
		summary.Avatar = avatarURL
		summary.AvatarRef = &BlobRef{
			CID:         blob.Ref.Link,
			ContentType: blob.MimeType,
			Size:        blob.Size,
		}
	}
	return summary, nil
}

// validatePDSURL rejects PDS endpoints that are not plain https origins.
// The IP-level SSRF guard lives in the dial hook (httpclient.go); this
// catches malformed schemes before a request is even attempted.
func validatePDSURL(pds string) error {
	parsed, err := url.Parse(pds)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("PDS endpoint must be an https origin: %q", pds)
	}
	return nil
}

func blobURL(pds, did, cid string) (string, error) {
	base, err := url.Parse(pds)
	if err != nil {
		return "", err
	}
	if (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" {
		return "", fmt.Errorf("invalid PDS URL")
	}
	base.Path = strings.TrimRight(base.Path, "/") +
		"/xrpc/com.atproto.sync.getBlob"
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	query := base.Query()
	query.Set("did", did)
	query.Set("cid", cid)
	base.RawQuery = query.Encode()
	return base.String(), nil
}

// NewDefaultService uses Bluesky profiles as the temporary authority policy.
func NewDefaultService() *Service {
	// One public-only client is shared by identity resolution and record
	// reads: every outbound host (PLC directory aside, handle domains, DID
	// service endpoints, PDS hosts) is account-controlled input.
	httpClient := newPublicOnlyHTTPClient()
	resolver := &atprotoResolver{
		directory: &identity.Directory{
			Resolver: &identity.DefaultResolver{
				HTTPClient: gt.Some(httpClient),
			},
		},
	}
	return NewService(
		resolver,
		&atprotoRecordReader{httpClient: httpClient},
		bsky.NSIDActorProfile,
		Source{
			Collection: bsky.NSIDActorProfile,
			RecordKey:  profileRecordKey,
			Extract:    extractBlueskyProfile,
		},
	)
}
