package domains

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings/settingstest"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store/storetest"
)

const (
	paramPath   = "/stack/domains"
	adminDomain = "admin.short.example.com"
	zoneID      = "Z123"
)

var fixedNow = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

// calls counts calls per operation; errs makes an operation fail.
type calls struct {
	mu    sync.Mutex
	count map[string]int
	errs  map[string]error
}

func (c *calls) enter(op string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.count == nil {
		c.count = map[string]int{}
	}
	c.count[op]++
	return c.errs[op]
}

func (c *calls) fail(op string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errs == nil {
		c.errs = map[string]error{}
	}
	c.errs[op] = err
}

func (c *calls) n(op string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count[op]
}

func (c *calls) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.count {
		n += v
	}
	return n
}

type fakeCerts struct {
	calls
	certs    map[string]Cert
	untagged map[string]bool
	// inUse is how many Delete calls answer ErrCertInUse before one succeeds;
	// negative means always.
	inUse   int
	deleted []string
}

func newFakeCerts() *fakeCerts {
	return &fakeCerts{certs: map[string]Cert{}, untagged: map[string]bool{}}
}

func certARN(domain string) string {
	return "arn:aws:acm:eu-central-1:111111111111:certificate/" + domain
}

func (f *fakeCerts) Request(_ context.Context, domain string) (string, error) {
	if err := f.enter("Request"); err != nil {
		return "", err
	}
	arn := certARN(domain)
	if _, ok := f.certs[arn]; !ok {
		f.certs[arn] = Cert{Status: CertPendingValidation}
	}
	return arn, nil
}

func (f *fakeCerts) Describe(_ context.Context, arn string) (Cert, error) {
	if err := f.enter("Describe"); err != nil {
		return Cert{}, err
	}
	c, ok := f.certs[arn]
	if !ok {
		return Cert{}, errors.New("no such certificate")
	}
	return c, nil
}

func (f *fakeCerts) Delete(_ context.Context, arn string) (bool, error) {
	if err := f.enter("Delete"); err != nil {
		return false, err
	}
	if f.untagged[arn] {
		return false, nil
	}
	if f.inUse != 0 {
		if f.inUse > 0 {
			f.inUse--
		}
		return false, ErrCertInUse
	}
	delete(f.certs, arn)
	f.deleted = append(f.deleted, arn)
	return true, nil
}

// issue sets the certificate of domain to status with validation records.
func (f *fakeCerts) issue(domain, status string) {
	f.certs[certARN(domain)] = Cert{Status: status, Records: validation(domain)}
}

func validation(domain string) []settings.Record {
	return []settings.Record{{Name: "_abc." + domain + ".", Type: "CNAME", Value: "_xyz.acm-validations.aws."}}
}

type fakeGateway struct {
	calls
	domains map[string]string // domain -> cert ARN
}

func target(domain string) string { return "d-" + domain + ".execute-api.eu-central-1.amazonaws.com" }

func endpoint(domain string) Endpoint {
	return Endpoint{Target: target(domain), HostedZoneID: "ZGATEWAY"}
}

func (f *fakeGateway) EnsureDomain(_ context.Context, domain, cert string) (Endpoint, error) {
	if err := f.enter("EnsureDomain"); err != nil {
		return Endpoint{}, err
	}
	if f.domains == nil {
		f.domains = map[string]string{}
	}
	if _, ok := f.domains[domain]; !ok {
		f.domains[domain] = cert
		f.enter("created")
	}
	return endpoint(domain), nil
}

func (f *fakeGateway) FindDomain(_ context.Context, domain string) (Endpoint, bool, error) {
	if err := f.enter("FindDomain"); err != nil {
		return Endpoint{}, false, err
	}
	if _, ok := f.domains[domain]; !ok {
		return Endpoint{}, false, nil
	}
	return endpoint(domain), true, nil
}

func (f *fakeGateway) DeleteDomain(_ context.Context, domain string) error {
	if err := f.enter("DeleteDomain"); err != nil {
		return err
	}
	delete(f.domains, domain)
	return nil
}

type fakeZones struct {
	calls
	records map[string]string // zone|name|type -> value
}

func rkey(zone, name, typ string) string { return zone + "|" + name + "|" + typ }

func (f *fakeZones) set(k, v string) {
	if f.records == nil {
		f.records = map[string]string{}
	}
	f.records[k] = v
}

func (f *fakeZones) UpsertCNAME(_ context.Context, zone string, r settings.Record) error {
	if err := f.enter("UpsertCNAME"); err != nil {
		return err
	}
	f.set(rkey(zone, r.Name, r.Type), r.Value)
	return nil
}

func (f *fakeZones) UpsertAlias(_ context.Context, zone, name string, t Endpoint) error {
	if err := f.enter("UpsertAlias"); err != nil {
		return err
	}
	f.set(rkey(zone, name, "A"), t.Target)
	return nil
}

func (f *fakeZones) DeleteCNAME(_ context.Context, zone string, r settings.Record) error {
	if err := f.enter("DeleteCNAME"); err != nil {
		return err
	}
	if k := rkey(zone, r.Name, r.Type); f.records[k] == r.Value {
		delete(f.records, k)
	}
	return nil
}

func (f *fakeZones) DeleteAlias(_ context.Context, zone, name string, t Endpoint) error {
	if err := f.enter("DeleteAlias"); err != nil {
		return err
	}
	if k := rkey(zone, name, "A"); f.records[k] == t.Target {
		delete(f.records, k)
	}
	return nil
}

// fakeResolver answers from maps; a name missing from cnames is its own
// canonical name, as with net.Resolver.LookupCNAME for a name with A records.
type fakeResolver struct {
	cnames map[string]string
	hosts  map[string][]string
}

func (f *fakeResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	if c, ok := f.cnames[host]; ok {
		return c, nil
	}
	if _, ok := f.hosts[host]; ok {
		return host + ".", nil
	}
	return "", errors.New("no such host")
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if a, ok := f.hosts[host]; ok {
		return a, nil
	}
	return nil, errors.New("no such host")
}

type env struct {
	m        *Manager
	ssm      *settingstest.SSM
	certs    *fakeCerts
	gateway  *fakeGateway
	zones    *fakeZones
	resolver *fakeResolver
	links    *storetest.Fake
	sleeps   int
}

func newEnv(t *testing.T, route53Zones ...string) *env {
	t.Helper()
	e := &env{
		ssm:      settingstest.New(),
		certs:    newFakeCerts(),
		gateway:  &fakeGateway{},
		zones:    &fakeZones{},
		resolver: &fakeResolver{cnames: map[string]string{}, hosts: map[string][]string{}},
		links:    storetest.NewFake(),
	}
	e.m = &Manager{
		Settings:     e.ssm.Store(paramPath),
		Certs:        e.certs,
		Gateway:      e.gateway,
		Zones:        e.zones,
		Resolver:     e.resolver,
		Links:        e.links,
		Route53Zones: route53Zones,
		AdminDomain:  adminDomain,
		Now:          func() time.Time { return fixedNow },
		Sleep:        func(time.Duration) { e.sleeps++ },
	}
	e.ssm.Put(paramPath, settings.Domain{
		Domain: "short.example.com", Role: settings.RolePrimary, DNSMode: settings.DNSExternal,
		Status: settings.StatusActive, FallbackURL: "https://example.com/",
	})
	return e
}

// stored returns the stored setting of domain.
func (e *env) stored(t *testing.T, domain string) settings.Domain {
	t.Helper()
	d, ok, err := e.ssm.Store(paramPath).Get(context.Background(), domain)
	if err != nil || !ok {
		t.Fatalf("Get(%s) = %v, %v", domain, ok, err)
	}
	return d
}

func (e *env) addLink(domain, slug string) {
	_ = e.links.CreateLink(context.Background(), store.Link{Domain: domain, Slug: slug, TargetURL: "https://example.com/x"})
}

func wantStatus(t *testing.T, d settings.Domain, status string) {
	t.Helper()
	if d.Status != status {
		t.Fatalf("status = %q, want %q (error %q)", d.Status, status, d.Error)
	}
}

func wantErr(t *testing.T, err error, status int) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Status != status {
		t.Fatalf("error = %v, want status %d", err, status)
	}
	return e
}
