// Package profile retrieves profile records from AT Protocol repositories.
package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

var (
	// ErrIdentityNotFound indicates that an AT Protocol identity was not found.
	ErrIdentityNotFound = errors.New("identity not found")
	// ErrNoPDS indicates that an identity does not declare a PDS.
	ErrNoPDS = errors.New("identity has no PDS")
	// ErrRecordNotFound indicates that a repository record was not found.
	ErrRecordNotFound = errors.New("record not found")
	// ErrUnsupportedCollection indicates that a collection is not allowlisted.
	ErrUnsupportedCollection = errors.New("unsupported profile collection")
)

// Source describes an allowlisted profile record type.
type Source struct {
	Collection string
	RecordKey  string
	Extract    func(Identity, json.RawMessage) (Summary, error)
}

// Summary contains canonical fields extracted from the selected profile.
type Summary struct {
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
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
	DID           string   `json:"did"`
	Handle        string   `json:"handle,omitempty"`
	PDS           string   `json:"pds"`
	Authoritative string   `json:"authoritative,omitempty"`
	DisplayName   string   `json:"displayName,omitempty"`
	Description   string   `json:"description,omitempty"`
	Avatar        string   `json:"avatar,omitempty"`
	Profiles      []Record `json:"profiles"`
}

type identityResolver interface {
	Resolve(context.Context, string) (Identity, error)
}

type recordReader interface {
	Get(context.Context, Identity, Source) (Record, error)
}

// Service retrieves profile records from allowlisted collections.
type Service struct {
	resolver      identityResolver
	reader        recordReader
	authoritative string
	sources       map[string]Source
	order         []string
}

// NewService constructs a profile service with a temporary authority policy.
func NewService(
	resolver identityResolver,
	reader recordReader,
	authoritative string,
	sources ...Source,
) *Service {
	service := &Service{
		resolver:      resolver,
		reader:        reader,
		authoritative: authoritative,
		sources:       make(map[string]Source, len(sources)),
		order:         make([]string, 0, len(sources)),
	}
	for _, source := range sources {
		service.sources[source.Collection] = source
		service.order = append(service.order, source.Collection)
	}
	return service
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

	for _, source := range sources {
		record, getErr := s.reader.Get(ctx, identity, source)
		if errors.Is(getErr, ErrRecordNotFound) {
			continue
		}
		if getErr != nil {
			return Account{}, fmt.Errorf("get %s: %w", source.Collection, getErr)
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
		}
		account.Profiles = append(account.Profiles, record)
		if record.Collection == s.authoritative {
			account.Authoritative = record.Collection
			applySummary(&account, summary)
		} else if len(account.Profiles) == 1 {
			applySummary(&account, summary)
		}
	}
	return account, nil
}

func applySummary(account *Account, summary Summary) {
	account.DisplayName = summary.DisplayName
	account.Description = summary.Description
	account.Avatar = summary.Avatar
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
