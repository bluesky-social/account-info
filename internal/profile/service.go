// Package profile retrieves profile records from AT Protocol repositories.
package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
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
	// ErrProfileCreatedAt indicates that multiple profiles cannot be ordered by age.
	ErrProfileCreatedAt = errors.New("profile createdAt is missing or invalid")
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
	App               *ProfileApp
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

// ProfileApp describes how a profile record links to its application.
type ProfileApp struct {
	Name string
	// Icon names the self-hosted assets/apps/<icon>.svg presentation asset.
	Icon       string
	ProfileURL func(Identity) (string, error)
}

// AppLink is a resolved application profile link for a record.
type AppLink struct {
	Name string
	Icon string
	URL  string
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
	App        *AppLink        `json:"-"`
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
		if source.App != nil {
			appLink, linkErr := resolveAppLink(identity, source.App)
			if linkErr != nil {
				return Account{}, fmt.Errorf(
					"link %s: %w",
					source.Collection,
					linkErr,
				)
			}
			record.App = &appLink
		}
		var summary Summary
		if source.Extract != nil {
			summary, getErr = source.Extract(identity, record.Value)
			if getErr != nil {
				return Account{}, fmt.Errorf(
					"extract %s: %w",
					source.Collection,
					getErr,
				)
			}
		} else if source.Selectors.configured() {
			summary, getErr = extractJSONProfile(
				identity,
				source.Collection,
				record.Value,
				source.compiledSelectors,
			)
			if getErr != nil {
				return Account{}, fmt.Errorf(
					"extract %s: %w",
					source.Collection,
					getErr,
				)
			}
		}
		account.Profiles = append(account.Profiles, record)
		summaries = append(summaries, summary)
	}
	if err := selectDefaultProfile(&account, summaries); err != nil {
		return Account{}, err
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

	selected := 0
	if len(account.Profiles) > 1 {
		oldest, err := parseProfileCreatedAt(
			account.Profiles[0].Collection,
			summaries[0].CreatedAt,
		)
		if err != nil {
			return err
		}
		for index := 1; index < len(account.Profiles); index++ {
			createdAt, parseErr := parseProfileCreatedAt(
				account.Profiles[index].Collection,
				summaries[index].CreatedAt,
			)
			if parseErr != nil {
				return parseErr
			}
			if createdAt.Before(oldest) ||
				(createdAt.Equal(oldest) &&
					account.Profiles[index].Collection < account.Profiles[selected].Collection) {
				selected = index
				oldest = createdAt
			}
		}
	}

	account.Default = account.Profiles[selected].Collection
	applySummary(account, summaries[selected])
	return nil
}

func parseProfileCreatedAt(collection, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, fmt.Errorf("%w: %s", ErrProfileCreatedAt, collection)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%w: %s: %w",
			ErrProfileCreatedAt,
			collection,
			err,
		)
	}
	return createdAt, nil
}

func resolveAppLink(identity Identity, app *ProfileApp) (AppLink, error) {
	if app.Name == "" {
		return AppLink{}, fmt.Errorf("app name is empty")
	}
	if !validAppIcon(app.Icon) {
		return AppLink{}, fmt.Errorf("invalid app icon %q", app.Icon)
	}
	if app.ProfileURL == nil {
		return AppLink{}, fmt.Errorf("app profile URL builder is nil")
	}
	rawURL, err := app.ProfileURL(identity)
	if err != nil {
		return AppLink{}, fmt.Errorf("build app profile URL: %w", err)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return AppLink{}, fmt.Errorf("invalid app profile URL %q", rawURL)
	}
	return AppLink{Name: app.Name, Icon: app.Icon, URL: parsed.String()}, nil
}

func validAppIcon(icon string) bool {
	if icon == "" {
		return false
	}
	for _, character := range icon {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789-", character) {
			return false
		}
	}
	return true
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
