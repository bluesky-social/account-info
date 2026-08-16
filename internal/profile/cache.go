package profile

import (
	"container/list"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CacheConfig controls caching of account lookup results.
type CacheConfig struct {
	TTL        time.Duration
	ErrorTTL   time.Duration
	MaxEntries int
}

func (c CacheConfig) validate() error {
	if c.TTL <= 0 {
		return fmt.Errorf("cache TTL must be positive: %s", c.TTL)
	}
	if c.ErrorTTL <= 0 {
		return fmt.Errorf("cache error TTL must be positive: %s", c.ErrorTTL)
	}
	if c.ErrorTTL >= c.TTL {
		return fmt.Errorf(
			"cache error TTL %s must be shorter than cache TTL %s",
			c.ErrorTTL,
			c.TTL,
		)
	}
	if c.MaxEntries <= 0 {
		return fmt.Errorf(
			"cache maximum entries must be positive: %d",
			c.MaxEntries,
		)
	}
	return nil
}

type accountCacheKey struct {
	identifier  string
	collections string
}

type accountCacheEntry struct {
	key       accountCacheKey
	account   Account
	err       error
	expiresAt time.Time
}

type accountCache struct {
	mu         sync.Mutex
	entries    map[accountCacheKey]*list.Element
	recency    *list.List
	ttl        time.Duration
	errorTTL   time.Duration
	maxEntries int
	now        func() time.Time
}

func newAccountCache(
	ttl time.Duration,
	errorTTL time.Duration,
	maxEntries int,
) *accountCache {
	return &accountCache{
		entries:    make(map[accountCacheKey]*list.Element),
		recency:    list.New(),
		ttl:        ttl,
		errorTTL:   errorTTL,
		maxEntries: maxEntries,
		now:        time.Now,
	}
}

func (c *accountCache) get(
	identifier string,
	collections []string,
) (Account, error, bool) {
	key := newAccountCacheKey(identifier, collections)

	c.mu.Lock()
	element, ok := c.entries[key]
	if !ok {
		c.mu.Unlock()
		return Account{}, nil, false
	}
	entry := cacheEntry(element)
	if !c.now().Before(entry.expiresAt) {
		c.remove(element)
		c.mu.Unlock()
		return Account{}, nil, false
	}
	c.recency.MoveToFront(element)
	account := entry.account
	err := entry.err
	c.mu.Unlock()

	return cloneAccount(&account), err, true
}

func (c *accountCache) put(
	identifier string,
	collections []string,
	account *Account,
	err error,
) {
	key := newAccountCacheKey(identifier, collections)
	ttl := c.ttl
	if err != nil {
		ttl = c.errorTTL
	}
	entry := &accountCacheEntry{
		key:       key,
		account:   cloneAccount(account),
		err:       err,
		expiresAt: c.now().Add(ttl),
	}

	c.mu.Lock()
	if existing, ok := c.entries[key]; ok {
		existing.Value = entry
		c.recency.MoveToFront(existing)
		c.mu.Unlock()
		return
	}
	if len(c.entries) >= c.maxEntries {
		c.remove(c.recency.Back())
	}
	element := c.recency.PushFront(entry)
	c.entries[key] = element
	c.mu.Unlock()
}

func (c *accountCache) remove(element *list.Element) {
	entry := cacheEntry(element)
	delete(c.entries, entry.key)
	c.recency.Remove(element)
}

func cacheEntry(element *list.Element) *accountCacheEntry {
	entry, ok := element.Value.(*accountCacheEntry)
	if !ok {
		panic("profile: invalid account cache entry")
	}
	return entry
}

func newAccountCacheKey(
	identifier string,
	collections []string,
) accountCacheKey {
	var encoded strings.Builder
	for _, collection := range collections {
		encoded.WriteString(strconv.Itoa(len(collection)))
		encoded.WriteByte(':')
		encoded.WriteString(collection)
	}
	return accountCacheKey{
		identifier:  identifier,
		collections: encoded.String(),
	}
}

func cloneAccount(account *Account) Account {
	clone := *account
	clone.Profiles = make([]Record, len(account.Profiles))
	for i, record := range account.Profiles {
		clone.Profiles[i] = record
		clone.Profiles[i].Value = append([]byte(nil), record.Value...)
		if record.App != nil {
			appLink := *record.App
			clone.Profiles[i].App = &appLink
		}
	}
	if account.avatarRef != nil {
		avatarRef := *account.avatarRef
		clone.avatarRef = &avatarRef
	}
	return clone
}
