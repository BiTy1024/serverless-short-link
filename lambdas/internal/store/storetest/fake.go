// Package storetest provides an in-memory store for handler tests.
package storetest

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

// Fake keeps links in insertion order and clicks as appended. It mirrors the
// DynamoDB semantics the handlers rely on: links keyed by (domain, slug),
// conditional create, update and delete, and click queries returning clicks
// oldest first.
type Fake struct {
	mu     sync.Mutex
	links  []store.Link
	Clicks []store.Click

	// Errors makes the named method (e.g. "PutClick") fail with the given error.
	Errors map[string]error
	// LastRange is the range passed to the latest click read.
	LastRange store.TimeRange
	// LastDomain is the domain passed to the latest ListLinks, ListLinkKeys
	// or QueryDomainClicks call.
	LastDomain string
	// Calls counts calls per method name.
	Calls map[string]int
}

// NewFake returns a store holding links.
func NewFake(links ...store.Link) *Fake {
	return &Fake{links: slices.Clone(links), Calls: map[string]int{}}
}

// Links returns a copy of the stored links.
func (f *Fake) Links() []store.Link {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.links)
}

// enter records a call to method and returns its configured error. The
// caller holds f.mu.
func (f *Fake) enter(method string) error {
	if f.Calls == nil {
		f.Calls = map[string]int{}
	}
	f.Calls[method]++
	return f.Errors[method]
}

func (f *Fake) index(domain, slug string) int {
	return slices.IndexFunc(f.links, func(l store.Link) bool { return l.Domain == domain && l.Slug == slug })
}

func (f *Fake) GetLink(_ context.Context, domain, slug string) (store.Link, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetLink"); err != nil {
		return store.Link{}, false, err
	}
	i := f.index(domain, slug)
	if i < 0 {
		return store.Link{}, false, nil
	}
	return f.links[i], true, nil
}

func (f *Fake) linksOf(domain string) []store.Link {
	out := []store.Link{}
	for _, l := range f.links {
		if domain == "" || l.Domain == domain {
			out = append(out, l)
		}
	}
	return out
}

func (f *Fake) ListLinks(_ context.Context, domain string) ([]store.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LastDomain = domain
	if err := f.enter("ListLinks"); err != nil {
		return nil, err
	}
	return f.linksOf(domain), nil
}

func (f *Fake) ListLinkKeys(_ context.Context, domain string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LastDomain = domain
	if err := f.enter("ListLinkKeys"); err != nil {
		return nil, err
	}
	var keys []string
	for _, l := range f.linksOf(domain) {
		keys = append(keys, store.LinkKey(l.Domain, l.Slug))
	}
	return keys, nil
}

func (f *Fake) CreateLink(_ context.Context, link store.Link) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CreateLink"); err != nil {
		return err
	}
	if f.index(link.Domain, link.Slug) >= 0 {
		return store.ErrLinkExists
	}
	f.links = append(f.links, link)
	return nil
}

func (f *Fake) UpdateLinkTarget(_ context.Context, domain, slug, targetURL, updatedAt string) (store.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("UpdateLinkTarget"); err != nil {
		return store.Link{}, err
	}
	i := f.index(domain, slug)
	if i < 0 {
		return store.Link{}, store.ErrLinkNotFound
	}
	f.links[i].TargetURL = targetURL
	f.links[i].UpdatedAt = updatedAt
	return f.links[i], nil
}

func (f *Fake) DeleteLink(_ context.Context, domain, slug string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("DeleteLink"); err != nil {
		return err
	}
	i := f.index(domain, slug)
	if i < 0 {
		return store.ErrLinkNotFound
	}
	f.links = slices.Delete(f.links, i, i+1)
	return nil
}

func (f *Fake) PutClick(_ context.Context, click store.Click) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("PutClick"); err != nil {
		return err
	}
	f.Clicks = append(f.Clicks, click)
	return nil
}

// ScanClicks returns the clicks within r in insertion order, as a Scan with a
// filter returns them unordered.
func (f *Fake) ScanClicks(_ context.Context, r store.TimeRange) ([]store.Click, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LastRange = r
	if err := f.enter("ScanClicks"); err != nil {
		return nil, err
	}
	return f.clicks(func(store.Click) bool { return true }, r, false), nil
}

func (f *Fake) QueryDomainClicks(_ context.Context, domain string, r store.TimeRange) ([]store.Click, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LastRange = r
	f.LastDomain = domain
	if err := f.enter("QueryDomainClicks"); err != nil {
		return nil, err
	}
	return f.clicks(func(c store.Click) bool { return c.Domain == domain }, r, true), nil
}

func (f *Fake) QueryLinkClicks(_ context.Context, domain, slug string, r store.TimeRange) ([]store.Click, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LastRange = r
	if err := f.enter("QueryLinkClicks"); err != nil {
		return nil, err
	}
	key := store.LinkKey(domain, slug)
	return f.clicks(func(c store.Click) bool { return c.LinkKey == key }, r, true), nil
}

func (f *Fake) clicks(match func(store.Click) bool, r store.TimeRange, sorted bool) []store.Click {
	var out []store.Click
	for _, c := range f.Clicks {
		if match(c) && r.Contains(c.Timestamp) {
			out = append(out, c)
		}
	}
	if sorted {
		slices.SortStableFunc(out, func(a, b store.Click) int { return strings.Compare(a.Timestamp, b.Timestamp) })
	}
	return out
}
