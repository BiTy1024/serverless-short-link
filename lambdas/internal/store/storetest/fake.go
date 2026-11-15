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
// DynamoDB semantics the handlers rely on: conditional create, update and
// delete, and queries returning clicks oldest first.
type Fake struct {
	mu     sync.Mutex
	links  []store.Link
	Clicks []store.Click

	// Errors makes the named method (e.g. "PutClick") fail with the given error.
	Errors map[string]error
	// LastRange is the range passed to the latest QueryClicks call.
	LastRange store.TimeRange
	// GetLinkCalls counts GetLink calls.
	GetLinkCalls int
}

// NewFake returns a store holding links.
func NewFake(links ...store.Link) *Fake {
	return &Fake{links: slices.Clone(links)}
}

// Links returns a copy of the stored links.
func (f *Fake) Links() []store.Link {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.links)
}

func (f *Fake) index(shortPath string) int {
	return slices.IndexFunc(f.links, func(l store.Link) bool { return l.ShortPath == shortPath })
}

func (f *Fake) GetLink(_ context.Context, shortPath string) (store.Link, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.GetLinkCalls++
	if err := f.Errors["GetLink"]; err != nil {
		return store.Link{}, false, err
	}
	i := f.index(shortPath)
	if i < 0 {
		return store.Link{}, false, nil
	}
	return f.links[i], true, nil
}

func (f *Fake) ListLinks(context.Context) ([]store.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Errors["ListLinks"]; err != nil {
		return nil, err
	}
	return slices.Clone(f.links), nil
}

func (f *Fake) ListLinkPaths(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Errors["ListLinkPaths"]; err != nil {
		return nil, err
	}
	var paths []string
	for _, l := range f.links {
		paths = append(paths, l.ShortPath)
	}
	return paths, nil
}

func (f *Fake) CreateLink(_ context.Context, link store.Link) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Errors["CreateLink"]; err != nil {
		return err
	}
	if f.index(link.ShortPath) >= 0 {
		return store.ErrLinkExists
	}
	f.links = append(f.links, link)
	return nil
}

func (f *Fake) UpdateLinkTarget(_ context.Context, shortPath, targetURL, updatedAt string) (store.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Errors["UpdateLinkTarget"]; err != nil {
		return store.Link{}, err
	}
	i := f.index(shortPath)
	if i < 0 {
		return store.Link{}, store.ErrLinkNotFound
	}
	f.links[i].TargetURL = targetURL
	f.links[i].UpdatedAt = updatedAt
	return f.links[i], nil
}

func (f *Fake) DeleteLink(_ context.Context, shortPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Errors["DeleteLink"]; err != nil {
		return err
	}
	i := f.index(shortPath)
	if i < 0 {
		return store.ErrLinkNotFound
	}
	f.links = slices.Delete(f.links, i, i+1)
	return nil
}

func (f *Fake) PutClick(_ context.Context, click store.Click) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Errors["PutClick"]; err != nil {
		return err
	}
	f.Clicks = append(f.Clicks, click)
	return nil
}

func (f *Fake) ScanClicks(context.Context) ([]store.Click, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.Errors["ScanClicks"]; err != nil {
		return nil, err
	}
	return slices.Clone(f.Clicks), nil
}

func (f *Fake) QueryClicks(_ context.Context, redirectPath string, r store.TimeRange) ([]store.Click, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LastRange = r
	if err := f.Errors["QueryClicks"]; err != nil {
		return nil, err
	}
	var out []store.Click
	for _, c := range f.Clicks {
		if c.RedirectPath != redirectPath {
			continue
		}
		if r.Start != "" && c.Timestamp < r.Start {
			continue
		}
		if r.End != "" && c.Timestamp > r.End {
			continue
		}
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(a, b store.Click) int { return strings.Compare(a.Timestamp, b.Timestamp) })
	return out, nil
}
