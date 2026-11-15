// Package domains sets up the secondary domains of the stack: it requests a
// certificate for each, creates the API Gateway custom domain once the
// certificate is issued, writes or describes the DNS records the domain
// needs, and keeps the state in the domain settings.
//
// A secondary domain moves through these statuses:
//
//	pending_validation  certificate requested; waits for ACM to issue it
//	pending_dns         custom domain created; waits for the domain to point at it
//	active              served by the redirect
//	failed              ACM refused the certificate; delete and add the domain again
//
// In route53 mode the validation and alias records are written to the
// hosted zone, so pending_dns is passed in the same step. In external mode
// a person sets the records shown in validation_records and dns_records.
// The primary domain is set up by Terraform and never changed here.
package domains

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domain"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

// Certificate statuses ACM reports that the state machine acts on. Every
// other status ends in failed.
const (
	CertPendingValidation = "PENDING_VALIDATION"
	CertIssued            = "ISSUED"
)

// Cert is the state of an ACM certificate.
type Cert struct {
	Status        string
	FailureReason string
	// Records are the DNS validation records; ACM fills them in a few
	// seconds after the request, so they can be empty at first.
	Records []settings.Record
}

// ErrCertInUse is returned by Certs.Delete while a service still uses the
// certificate.
var ErrCertInUse = errors.New("certificate is still in use")

// Certs requests and removes the regional certificates of secondary domains.
type Certs interface {
	// Request asks for a DNS-validated certificate for domain, tagged with
	// the stack, and returns its ARN.
	Request(ctx context.Context, domain string) (arn string, err error)
	Describe(ctx context.Context, arn string) (Cert, error)
	// Delete removes the certificate when it carries the stack's tag;
	// deleted is false when it does not. A missing certificate counts as
	// deleted.
	Delete(ctx context.Context, arn string) (deleted bool, err error)
}

// Endpoint is the regional target of an API Gateway custom domain.
type Endpoint struct {
	Target       string
	HostedZoneID string
}

// Gateway manages the API Gateway custom domains of secondary domains.
type Gateway interface {
	// EnsureDomain creates the regional custom domain for domain with
	// certARN and maps it to the API's stage, unless they exist.
	EnsureDomain(ctx context.Context, domain, certARN string) (Endpoint, error)
	// FindDomain returns the custom domain's endpoint; found is false when
	// there is none.
	FindDomain(ctx context.Context, domain string) (ep Endpoint, found bool, err error)
	// DeleteDomain removes the API mappings and the custom domain; a missing
	// one is no error.
	DeleteDomain(ctx context.Context, domain string) error
}

// Zones writes and removes records in Route 53 hosted zones.
type Zones interface {
	UpsertCNAME(ctx context.Context, zoneID string, r settings.Record) error
	UpsertAlias(ctx context.Context, zoneID, name string, target Endpoint) error
	// DeleteCNAME and DeleteAlias remove the record only when it still has
	// the given value or target; a missing or changed record is left alone.
	DeleteCNAME(ctx context.Context, zoneID string, r settings.Record) error
	DeleteAlias(ctx context.Context, zoneID, name string, target Endpoint) error
}

// Resolver looks up public DNS; *net.Resolver implements it.
type Resolver interface {
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// Settings stores the domain settings; *settings.Store implements it.
type Settings interface {
	List(ctx context.Context) ([]settings.Domain, error)
	Get(ctx context.Context, domain string) (settings.Domain, bool, error)
	Create(ctx context.Context, d settings.Domain) error
	Put(ctx context.Context, d settings.Domain) error
	Delete(ctx context.Context, domain string) error
}

// Links lists the links of a domain; *store.Dynamo implements it.
type Links interface {
	ListLinkKeys(ctx context.Context, domain string) ([]string, error)
}

// Manager runs the domain operations.
type Manager struct {
	Settings Settings
	Certs    Certs
	Gateway  Gateway
	Zones    Zones
	Resolver Resolver
	Links    Links
	// Route53Zones are the hosted zones secondary domains may use in
	// route53 mode; empty disables route53 mode for secondaries.
	Route53Zones []string
	// AdminDomain is admin.<primary>, which cannot be added as a domain.
	AdminDomain string
	Now         func() time.Time
	// Sleep waits between attempts to delete a certificate that is still in
	// use right after its custom domain was removed.
	Sleep func(time.Duration)
}

// Error is a failure the caller can act on, with the HTTP status it maps to.
// LinkCount is set when a domain cannot be deleted because it has links.
type Error struct {
	Status    int
	Message   string
	LinkCount int
}

func (e *Error) Error() string { return e.Message }

func badRequest(format string, args ...any) *Error {
	return &Error{Status: 400, Message: fmt.Sprintf(format, args...)}
}

// certDeleteAttempts and certDeleteWait bound how long Delete waits for API
// Gateway to release the certificate of a removed custom domain.
const (
	certDeleteAttempts = 5
	certDeleteWait     = 3 * time.Second
)

// CreateRequest is the input of Create.
type CreateRequest struct {
	Domain       string
	DNSMode      string
	HostedZoneID string
	FallbackURL  string
}

// Create adds a secondary domain in pending_validation, requests its
// certificate and, in route53 mode, writes the validation records. A failed
// AWS call after the setting is stored is recorded in its error field; the
// next check retries it.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (settings.Domain, error) {
	name, err := domain.Parse(req.Domain)
	if err != nil {
		return settings.Domain{}, badRequest("%s", err.Error())
	}
	if name == m.AdminDomain {
		return settings.Domain{}, badRequest("Domain '%s' is the admin frontend's domain", name)
	}
	zone := strings.TrimSpace(req.HostedZoneID)
	switch req.DNSMode {
	case settings.DNSExternal:
		if zone != "" {
			return settings.Domain{}, badRequest("hosted_zone_id is only used with dns_mode 'route53'")
		}
	case settings.DNSRoute53:
		if len(m.Route53Zones) == 0 {
			return settings.Domain{}, badRequest("Route 53 mode is disabled: the Terraform variable route53_zone_ids lists no hosted zones")
		}
		if !slices.Contains(m.Route53Zones, zone) {
			return settings.Domain{}, badRequest("Hosted zone '%s' is not one of the zones in route53_zone_ids: %s", zone, strings.Join(m.Route53Zones, ", "))
		}
	default:
		return settings.Domain{}, badRequest("dns_mode must be 'external' or 'route53'")
	}
	if msg := validateURL(req.FallbackURL); msg != "" {
		return settings.Domain{}, badRequest("%s", msg)
	}

	d := settings.Domain{
		Domain:       name,
		Role:         settings.RoleSecondary,
		DNSMode:      req.DNSMode,
		HostedZoneID: zone,
		FallbackURL:  req.FallbackURL,
		Status:       settings.StatusPendingValidation,
		UpdatedAt:    store.Timestamp(m.Now()),
	}
	err = m.Settings.Create(ctx, d)
	if errors.Is(err, settings.ErrExists) {
		return settings.Domain{}, &Error{Status: 409, Message: fmt.Sprintf("Domain '%s' already exists", name)}
	}
	if err != nil {
		return settings.Domain{}, err
	}
	slog.Info("Created domain", "domain", name, "dns_mode", d.DNSMode)
	return m.advanceAndSave(ctx, d)
}

// List returns every domain, the primary first.
func (m *Manager) List(ctx context.Context) ([]settings.Domain, error) {
	return m.Settings.List(ctx)
}

// Check advances a domain as far as its certificate and DNS allow and stores
// the result. It is idempotent: an active, failed or primary domain is
// returned unchanged.
func (m *Manager) Check(ctx context.Context, name string) (settings.Domain, error) {
	d, err := m.get(ctx, name)
	if err != nil {
		return settings.Domain{}, err
	}
	return m.advanceAndSave(ctx, d)
}

// CheckAll checks every secondary domain that is pending. It goes on after
// a failing domain and returns the errors joined.
func (m *Manager) CheckAll(ctx context.Context) error {
	all, err := m.Settings.List(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, d := range all {
		if !pending(d) {
			continue
		}
		got, err := m.advanceAndSave(ctx, d)
		if err != nil {
			slog.Error("Checking domain failed", "domain", d.Domain, "error", err.Error())
			errs = append(errs, fmt.Errorf("%s: %w", d.Domain, err))
			continue
		}
		slog.Info("Checked domain", "domain", d.Domain, "status", got.Status, "error", got.Error)
	}
	return errors.Join(errs...)
}

// UpdateFallback sets the fallback URL of a secondary domain. The primary's
// fallback is the Terraform variable default_redirect_url.
func (m *Manager) UpdateFallback(ctx context.Context, name, url string) (settings.Domain, error) {
	d, err := m.get(ctx, name)
	if err != nil {
		return settings.Domain{}, err
	}
	if d.Role == settings.RolePrimary {
		return settings.Domain{}, &Error{Status: 409, Message: "The primary domain is managed by Terraform; change default_redirect_url there"}
	}
	if msg := validateURL(url); msg != "" {
		return settings.Domain{}, badRequest("%s", msg)
	}
	d.FallbackURL = url
	d.UpdatedAt = store.Timestamp(m.Now())
	if err := m.Settings.Put(ctx, d); err != nil {
		return settings.Domain{}, err
	}
	slog.Info("Updated domain fallback", "domain", name, "fallback_url", url)
	return d, nil
}

// Delete removes a secondary domain without links: the Route 53 records it
// wrote, the custom domain, the certificate (when it carries the stack's
// tag) and the setting. Every step tolerates work already done, so a delete
// that stopped half way can be repeated.
func (m *Manager) Delete(ctx context.Context, name string) error {
	d, err := m.get(ctx, name)
	if err != nil {
		return err
	}
	if d.Role == settings.RolePrimary {
		return &Error{Status: 409, Message: "The primary domain is managed by Terraform and cannot be deleted here"}
	}
	keys, err := m.Links.ListLinkKeys(ctx, name)
	if err != nil {
		return err
	}
	if n := len(keys); n > 0 {
		return &Error{Status: 409, Message: fmt.Sprintf("Domain '%s' still has %d links; delete them first", name, n), LinkCount: n}
	}

	if d.DNSMode == settings.DNSRoute53 {
		ep, found, err := m.Gateway.FindDomain(ctx, name)
		if err != nil {
			return err
		}
		if found {
			if err := m.Zones.DeleteAlias(ctx, d.HostedZoneID, name, ep); err != nil {
				return err
			}
		}
		for _, r := range d.ValidationRecords {
			if err := m.Zones.DeleteCNAME(ctx, d.HostedZoneID, r); err != nil {
				return err
			}
		}
	}
	if err := m.Gateway.DeleteDomain(ctx, name); err != nil {
		return err
	}
	if d.CertARN != "" {
		if err := m.deleteCert(ctx, d); err != nil {
			return err
		}
	}
	if err := m.Settings.Delete(ctx, name); err != nil && !errors.Is(err, settings.ErrNotFound) {
		return err
	}
	slog.Info("Deleted domain", "domain", name)
	return nil
}

func (m *Manager) deleteCert(ctx context.Context, d settings.Domain) error {
	for attempt := 1; ; attempt++ {
		deleted, err := m.Certs.Delete(ctx, d.CertARN)
		switch {
		case err == nil && !deleted:
			slog.Warn("Certificate lacks the stack tag, left in place", "domain", d.Domain, "cert_arn", d.CertARN)
			return nil
		case err == nil:
			return nil
		case !errors.Is(err, ErrCertInUse):
			return err
		case attempt == certDeleteAttempts:
			return &Error{Status: 503, Message: fmt.Sprintf("The certificate of '%s' is still in use by API Gateway; delete the domain again in a minute", d.Domain)}
		}
		m.Sleep(certDeleteWait)
	}
}

func (m *Manager) get(ctx context.Context, raw string) (settings.Domain, error) {
	name, err := domain.Parse(raw)
	if err != nil {
		return settings.Domain{}, badRequest("%s", err.Error())
	}
	d, ok, err := m.Settings.Get(ctx, name)
	if err != nil {
		return settings.Domain{}, err
	}
	if !ok {
		return settings.Domain{}, &Error{Status: 404, Message: fmt.Sprintf("Domain '%s' not found", name)}
	}
	return d, nil
}

func pending(d settings.Domain) bool {
	return d.Role != settings.RolePrimary &&
		(d.Status == settings.StatusPendingValidation || d.Status == settings.StatusPendingDNS)
}

// advanceAndSave runs advance and stores the result when it changed and the
// domain still exists. A failing AWS call is recorded in the error field and
// stored with the progress made before it; it does not fail the call.
func (m *Manager) advanceAndSave(ctx context.Context, d settings.Domain) (settings.Domain, error) {
	if !pending(d) {
		return d, nil
	}
	before := d
	next, err := m.advance(ctx, d)
	switch {
	case err != nil:
		slog.Error("Advancing domain failed", "domain", d.Domain, "status", next.Status, "error", err.Error())
		next.Error = err.Error()
	case next.Status != settings.StatusFailed:
		next.Error = ""
	}
	if equal(before, next) {
		return next, nil
	}
	// A delete may have run while this check talked to AWS; writing now
	// would bring the deleted domain back.
	if _, ok, err := m.Settings.Get(ctx, d.Domain); err != nil {
		return settings.Domain{}, err
	} else if !ok {
		slog.Info("Domain deleted during check, result dropped", "domain", d.Domain)
		return next, nil
	}
	next.UpdatedAt = store.Timestamp(m.Now())
	if err := m.Settings.Put(ctx, next); err != nil {
		return settings.Domain{}, err
	}
	if next.Status != before.Status {
		slog.Info("Domain status changed", "domain", d.Domain, "from", before.Status, "to", next.Status)
	}
	return next, nil
}

func equal(a, b settings.Domain) bool {
	return a.Status == b.Status && a.CertARN == b.CertARN && a.TargetDomain == b.TargetDomain &&
		a.Error == b.Error && slices.Equal(a.ValidationRecords, b.ValidationRecords) &&
		slices.Equal(a.DNSRecords, b.DNSRecords)
}

// advance moves a pending domain forward. On error it returns the domain
// with the progress made so far.
func (m *Manager) advance(ctx context.Context, d settings.Domain) (settings.Domain, error) {
	if d.Status == settings.StatusPendingValidation {
		if d.CertARN == "" {
			arn, err := m.Certs.Request(ctx, d.Domain)
			if err != nil {
				return d, fmt.Errorf("requesting certificate: %w", err)
			}
			d.CertARN = arn
		}
		cert, err := m.Certs.Describe(ctx, d.CertARN)
		if err != nil {
			return d, fmt.Errorf("reading certificate: %w", err)
		}
		if len(cert.Records) > 0 {
			d.ValidationRecords = cert.Records
		}
		if d.DNSMode == settings.DNSRoute53 {
			for _, r := range d.ValidationRecords {
				if err := m.Zones.UpsertCNAME(ctx, d.HostedZoneID, r); err != nil {
					return d, fmt.Errorf("writing validation record: %w", err)
				}
			}
		}
		switch cert.Status {
		case CertPendingValidation:
			return d, nil
		case CertIssued:
			d.Status = settings.StatusPendingDNS
		default:
			d.Status = settings.StatusFailed
			d.Error = "Certificate " + cert.Status
			if cert.FailureReason != "" {
				d.Error += ": " + cert.FailureReason
			}
			return d, nil
		}
	}

	ep, err := m.Gateway.EnsureDomain(ctx, d.Domain, d.CertARN)
	if err != nil {
		return d, fmt.Errorf("creating custom domain: %w", err)
	}
	d.TargetDomain = ep.Target
	if d.DNSMode == settings.DNSRoute53 {
		if err := m.Zones.UpsertAlias(ctx, d.HostedZoneID, d.Domain, ep); err != nil {
			return d, fmt.Errorf("writing alias record: %w", err)
		}
		d.DNSRecords = []settings.Record{}
		d.Status = settings.StatusActive
		return d, nil
	}
	d.DNSRecords = []settings.Record{{Name: d.Domain, Type: "CNAME", Value: ep.Target}}
	if m.pointsAt(ctx, d.Domain, ep.Target) {
		d.Status = settings.StatusActive
	}
	return d, nil
}

// pointsAt reports whether public DNS sends name to target: both end in the
// same canonical name (a CNAME to target, possibly through further CNAMEs),
// or, for DNS hosts that flatten the CNAME into A records, both resolve to
// at least one common address. Lookup failures count as "not yet".
func (m *Manager) pointsAt(ctx context.Context, name, target string) bool {
	if got, err := m.Resolver.LookupCNAME(ctx, name); err == nil {
		want, err := m.Resolver.LookupCNAME(ctx, target)
		if err != nil {
			want = target
		}
		if canonical(got) == canonical(want) || canonical(got) == canonical(target) {
			return true
		}
	}
	got, err := m.Resolver.LookupHost(ctx, name)
	if err != nil {
		return false
	}
	want, err := m.Resolver.LookupHost(ctx, target)
	if err != nil {
		return false
	}
	for _, a := range got {
		if slices.ContainsFunc(want, func(b string) bool { return sameIP(a, b) }) {
			return true
		}
	}
	return false
}

func canonical(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

func sameIP(a, b string) bool {
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	return ia != nil && ia.Equal(ib)
}

// minURLLength is the length of the shortest accepted URL, "https://x.xx".
var minURLLength = len("https://x.xx")

// validateURL applies the rules of a link target to a fallback URL.
func validateURL(url string) string {
	switch {
	case url == "":
		return "fallback_url is required"
	case !strings.HasPrefix(url, "https://"):
		return "fallback_url must start with https://"
	case utf8.RuneCountInString(url) < minURLLength:
		return "fallback_url must be a valid URL with a domain"
	}
	return ""
}
