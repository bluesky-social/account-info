package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jcalabro/atmos"
	"github.com/jcalabro/atmos/api/bsky"
	"github.com/jcalabro/atmos/api/comatproto"
	"github.com/jcalabro/atmos/identity"
	"github.com/jcalabro/atmos/xrpc"
)

const profileRecordKey = "self"

type atprotoResolver struct {
	directory *identity.Directory
}

func (r *atprotoResolver) Resolve(
	ctx context.Context,
	raw string,
) (Identity, error) {
	identifier, err := atmos.ParseATIdentifier(raw)
	if err != nil {
		return Identity{}, fmt.Errorf("parse identifier: %w", err)
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

type atprotoRecordReader struct{}

func (*atprotoRecordReader) Get(
	ctx context.Context,
	account Identity,
	source Source,
) (Record, error) {
	client := &xrpc.Client{Host: account.PDS}
	output, err := comatproto.RepoGetRecord(
		ctx,
		client,
		"",
		source.Collection,
		account.DID,
		source.RecordKey,
	)
	if err != nil {
		var xrpcErr *xrpc.Error
		if errors.As(err, &xrpcErr) &&
			(xrpcErr.Name == comatproto.ErrRepoGetRecord_RecordNotFound ||
				xrpcErr.StatusCode == http.StatusNotFound) {
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
		avatarURL, err := blobURL(
			account.PDS,
			account.DID,
			record.Avatar.Val().Ref.Link,
		)
		if err != nil {
			return Summary{}, fmt.Errorf("build avatar URL: %w", err)
		}
		summary.Avatar = avatarURL
	}
	return summary, nil
}

func blobURL(pds string, did string, cid string) (string, error) {
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
	resolver := &atprotoResolver{
		directory: &identity.Directory{
			Resolver: &identity.DefaultResolver{},
		},
	}
	return NewService(
		resolver,
		&atprotoRecordReader{},
		bsky.NSIDActorProfile,
		Source{
			Collection: bsky.NSIDActorProfile,
			RecordKey:  profileRecordKey,
			Extract:    extractBlueskyProfile,
		},
	)
}
