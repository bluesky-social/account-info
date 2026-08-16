// Package profile retrieves profile records from AT Protocol repositories.
package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jcalabro/atmos"
)

var (
	// ErrInvalidIdentifier indicates a malformed account identifier.
	ErrInvalidIdentifier = errors.New("invalid identifier")
	// ErrIdentityNotFound indicates that an AT Protocol identity was not found.
	ErrIdentityNotFound = errors.New("identity not found")
	// ErrNoPDS indicates that an identity does not declare a PDS.
	ErrNoPDS = errors.New("identity has no PDS")
	// ErrRecordNotFound indicates that a repository record was not found.
	ErrRecordNotFound = errors.New("record not found")
	// ErrUnsupportedCollection indicates that a collection is not allowlisted.
	ErrUnsupportedCollection = errors.New("unsupported profile collection")
	// ErrProfileNotFound indicates that an account has no supported profile.
	ErrProfileNotFound = errors.New("profile not found")
	// ErrAvatarNotFound indicates that the selected profile has no avatar.
	ErrAvatarNotFound = errors.New("avatar not found")
)

// BlobRef identifies an avatar blob declared by a profile record.
type BlobRef struct {
	CID         string
	ContentType string
	Size        int64
}

// Avatar is a verified profile image ready for an HTTP response.
type Avatar struct {
	Content     []byte
	ContentType string
	CID         string
}

// Source describes an allowlisted profile record type.
type Source struct {
	Collection        string
	RecordKey         string
	Selectors         ProfileSelectors
	Extract           func(Identity, json.RawMessage) (Summary, error)
	compiledSelectors *compiledProfileSelectors
}

func (s *Source) validate() error {
	if s.Collection == "" {
		return fmt.Errorf("collection is empty")
	}
	if _, err := atmos.ParseNSID(s.Collection); err != nil {
		return fmt.Errorf("invalid collection: %w", err)
	}
	if s.RecordKey == "" {
		return fmt.Errorf("record key is empty")
	}
	if _, err := atmos.ParseRecordKey(s.RecordKey); err != nil {
		return fmt.Errorf("invalid record key: %w", err)
	}
	if s.Extract != nil && s.Selectors.configured() {
		return fmt.Errorf("both selectors and custom extractor are configured")
	}
	if s.Extract == nil && !s.Selectors.configured() {
		return fmt.Errorf("profile selectors are not configured")
	}
	if s.Selectors.configured() {
		compiled, err := s.Selectors.compile()
		if err != nil {
			return err
		}
		s.compiledSelectors = compiled
	}
	return nil
}

// Summary contains canonical fields extracted from the selected profile.
type Summary struct {
	DisplayName string   `json:"displayName,omitempty"`
	Description string   `json:"description,omitempty"`
	Avatar      string   `json:"avatar,omitempty"`
	AvatarRef   *BlobRef `json:"-"`
	CreatedAt   string   `json:"-"`
}

// Identity contains the account location needed to retrieve profile records.
type Identity struct {
	DID    string
	Handle string
	PDS    string
}

// Record is one profile record retrieved from an account repository.
type Record struct {
	Collection string          `json:"collection"`
	URI        string          `json:"uri"`
	CID        string          `json:"cid,omitempty"`
	Value      json.RawMessage `json:"value"`
}

// Account contains a resolved identity and its available profile records.
type Account struct {
	DID         string `json:"did"`
	Handle      string `json:"handle,omitempty"`
	PDS         string `json:"pds"`
	Default     string `json:"default,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	// AvatarContentType describes Avatar without exposing the private blob
	// reference in JSON. Web representations use it to build a typed image URL.
	AvatarContentType string   `json:"-"`
	Profiles          []Record `json:"profiles"`
	avatarRef         *BlobRef
}

type identityResolver interface {
	Resolve(context.Context, string) (Identity, error)
}

type recordReader interface {
	Get(context.Context, Identity, *Source) (Record, error)
	GetBlob(context.Context, Identity, BlobRef) (Avatar, error)
}

// Service retrieves profile records from allowlisted collections.
type Service struct {
	resolver identityResolver
	reader   recordReader
	sources  map[string]Source
	order    []string
	cache    *accountCache
}

// NewService constructs a profile service from the known profile sources.
func NewService(
	resolver identityResolver,
	reader recordReader,
	sources ...Source,
) (*Service, error) {
	service := &Service{
		resolver: resolver,
		reader:   reader,
		sources:  make(map[string]Source, len(sources)),
		order:    make([]string, 0, len(sources)),
	}
	for _, source := range sources {
		if err := source.validate(); err != nil {
			return nil, fmt.Errorf("profile source %q: %w", source.Collection, err)
		}
		if _, exists := service.sources[source.Collection]; exists {
			return nil, fmt.Errorf("duplicate profile source %q", source.Collection)
		}
		service.order = append(service.order, source.Collection)
		service.sources[source.Collection] = source
	}
	return service, nil
}

// Avatar resolves an account and retrieves its selected profile image.
func (s *Service) Avatar(ctx context.Context, identifier string) (Avatar, error) {
	account, err := s.Lookup(ctx, identifier, nil)
	if err != nil {
		return Avatar{}, err
	}
	if len(account.Profiles) == 0 {
		return Avatar{}, ErrProfileNotFound
	}
	if account.avatarRef == nil {
		return Avatar{}, ErrAvatarNotFound
	}

	avatar, err := s.reader.GetBlob(ctx, Identity{
		DID:    account.DID,
		Handle: account.Handle,
		PDS:    account.PDS,
	}, *account.avatarRef)
	if err != nil {
		return Avatar{}, fmt.Errorf("get avatar: %w", err)
	}
	return avatar, nil
}

// Collections returns the allowlisted profile collections in lookup order.
func (s *Service) Collections() []string {
	return slices.Clone(s.order)
}

// Lookup resolves an account and retrieves its selected profile records.
func (s *Service) Lookup(
	ctx context.Context,
	identifier string,
	collections []string,
) (Account, error) {
	if s.cache != nil {
		if account, err, ok := s.cache.get(identifier, collections); ok {
			return account, err
		}
	}

	account, err := s.lookup(ctx, identifier, collections)
	if s.cache != nil &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		s.cache.put(identifier, collections, &account, err)
	}
	return account, err
}

func (s *Service) lookup(
	ctx context.Context,
	identifier string,
	collections []string,
) (Account, error) {
	identity, err := s.resolver.Resolve(ctx, identifier)
	if err != nil {
		return Account{}, err
	}
	if identity.PDS == "" {
		return Account{}, ErrNoPDS
	}

	sources, err := s.selectSources(collections)
	if err != nil {
		return Account{}, err
	}

	account := Account{
		DID:      identity.DID,
		Handle:   identity.Handle,
		PDS:      identity.PDS,
		Profiles: make([]Record, 0, len(sources)),
	}
	summaries := make([]Summary, 0, len(sources))

	for _, source := range sources {
		record, getErr := s.reader.Get(ctx, identity, &source)
		if errors.Is(getErr, ErrRecordNotFound) {
			continue
		}
		if getErr != nil {
			return Account{}, fmt.Errorf("get %s: %w", source.Collection, getErr)
		}
		var summary Summary
		if source.Extract != nil {
			summary, getErr = source.Extract(identity, record.Value)
		} else if source.Selectors.configured() {
			summary, getErr = extractJSONProfile(
				identity,
				source.Collection,
				record.Value,
				source.compiledSelectors,
			)
		}
		if getErr != nil {
			slog.WarnContext(
				ctx,
				"profile record contains invalid summary data",
				"did", identity.DID,
				"collection", source.Collection,
				"error", getErr,
			)
		}
		account.Profiles = append(account.Profiles, record)
		summaries = append(summaries, summary)
	}
	if err := selectDefaultProfile(&account, summaries); err != nil {
		return Account{}, err
	}
	if len(account.Profiles) > 1 && account.Default == "" {
		slog.WarnContext(
			ctx,
			"no profile has a valid createdAt; default profile is unset",
			"did", identity.DID,
			"profiles", len(account.Profiles),
		)
	}
	return account, nil
}

func selectDefaultProfile(account *Account, summaries []Summary) error {
	if len(account.Profiles) == 0 {
		return nil
	}
	if len(account.Profiles) != len(summaries) {
		return fmt.Errorf("profile and summary counts differ")
	}

	if len(account.Profiles) == 1 {
		account.Default = account.Profiles[0].Collection
		applySummary(account, summaries[0])
		return nil
	}

	selected := -1
	var oldest time.Time
	for index := range account.Profiles {
		createdAt, err := parseProfileCreatedAt(
			account.Profiles[index].Collection,
			summaries[index].CreatedAt,
		)
		if err != nil {
			continue
		}
		if selected == -1 || createdAt.Before(oldest) ||
			(createdAt.Equal(oldest) &&
				account.Profiles[index].Collection < account.Profiles[selected].Collection) {
			selected = index
			oldest = createdAt
		}
	}
	if selected == -1 {
		return nil
	}

	account.Default = account.Profiles[selected].Collection
	applySummary(account, summaries[selected])
	return nil
}

func parseProfileCreatedAt(collection, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, fmt.Errorf("%s: createdAt is missing", collection)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%s: createdAt is invalid: %w",
			collection,
			err,
		)
	}
	return createdAt, nil
}

func applySummary(account *Account, summary Summary) {
	account.DisplayName = summary.DisplayName
	account.Description = summary.Description
	account.Avatar = summary.Avatar
	account.avatarRef = summary.AvatarRef
	account.AvatarContentType = ""
	if summary.AvatarRef != nil {
		account.AvatarContentType = summary.AvatarRef.ContentType
	}
}

func (s *Service) selectSources(collections []string) ([]Source, error) {
	if len(collections) == 0 {
		collections = s.order
	}

	selected := make([]Source, 0, len(collections))
	seen := make(map[string]struct{}, len(collections))
	for _, collection := range collections {
		if _, ok := seen[collection]; ok {
			continue
		}
		source, ok := s.sources[collection]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedCollection, collection)
		}
		seen[collection] = struct{}{}
		selected = append(selected, source)
	}
	return selected, nil
}
