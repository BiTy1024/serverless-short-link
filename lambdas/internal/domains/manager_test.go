package domains

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
)

var ctx = context.Background()

const video = "video.example.org"

func createExternal(t *testing.T, e *env) settings.Domain {
	t.Helper()
	d, err := e.m.Create(ctx, CreateRequest{Domain: " Video.Example.org ", DNSMode: settings.DNSExternal, FallbackURL: "https://example.org/"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestExternalDomainLifecycle(t *testing.T) {
	e := newEnv(t)

	// Create: certificate requested, ACM has no validation records yet.
	d := createExternal(t, e)
	wantStatus(t, d, settings.StatusPendingValidation)
	if d.Domain != video || d.Role != settings.RoleSecondary || d.CertARN != certARN(video) || d.Error != "" {
		t.Fatalf("created = %+v", d)
	}
	if len(d.ValidationRecords) != 0 || e.stored(t, video).CertARN != certARN(video) {
		t.Fatalf("created = %+v", d)
	}

	// Check: ACM now shows the validation record; still pending.
	e.certs.certs[certARN(video)] = Cert{Status: CertPendingValidation, Records: validation(video)}
	d, err := e.m.Check(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, d, settings.StatusPendingValidation)
	if !slices.Equal(d.ValidationRecords, validation(video)) || !slices.Equal(e.stored(t, video).ValidationRecords, validation(video)) {
		t.Fatalf("validation records = %v", d.ValidationRecords)
	}
	if e.zones.total() != 0 {
		t.Error("external mode wrote Route 53 records")
	}

	// Check: certificate issued; custom domain created, CNAME not set yet.
	e.certs.issue(video, CertIssued)
	d, err = e.m.Check(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, d, settings.StatusPendingDNS)
	wantDNS := []settings.Record{{Name: video, Type: "CNAME", Value: target(video)}}
	if d.TargetDomain != target(video) || !slices.Equal(d.DNSRecords, wantDNS) || e.gateway.domains[video] != certARN(video) {
		t.Fatalf("after issue = %+v", d)
	}

	// Re-check without DNS change: nothing is stored again.
	puts := e.ssm.Count("PutParameter")
	d, err = e.m.Check(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, d, settings.StatusPendingDNS)
	if e.ssm.Count("PutParameter") != puts {
		t.Error("unchanged re-check wrote the parameter")
	}
	if e.gateway.n("created") != 1 {
		t.Errorf("custom domain created %d times", e.gateway.n("created"))
	}

	// The CNAME is set: active.
	e.resolver.cnames[video] = target(video) + "."
	d, err = e.m.Check(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, d, settings.StatusActive)
	wantStatus(t, e.stored(t, video), settings.StatusActive)

	// Active is final: a re-check calls nothing.
	before := e.certs.total() + e.gateway.total()
	puts = e.ssm.Count("PutParameter")
	if d, err = e.m.Check(ctx, video); err != nil || d.Status != settings.StatusActive {
		t.Fatalf("re-check active = %+v, %v", d, err)
	}
	if e.certs.total()+e.gateway.total() != before || e.ssm.Count("PutParameter") != puts {
		t.Error("re-check of an active domain called AWS or wrote the parameter")
	}
}

func TestExternalDNSWithFlattenedCNAME(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.certs.issue(video, CertIssued)
	e.resolver.hosts[target(video)] = []string{"192.0.2.1", "192.0.2.2"}
	e.resolver.hosts[video] = []string{"198.51.100.1"}

	d, _ := e.m.Check(ctx, video)
	wantStatus(t, d, settings.StatusPendingDNS)

	e.resolver.hosts[video] = []string{"192.0.2.2"}
	d, _ = e.m.Check(ctx, video)
	wantStatus(t, d, settings.StatusActive)
}

func TestExternalDNSThroughCNAMEChain(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.certs.issue(video, CertIssued)
	// The target itself is a CNAME; LookupCNAME follows both chains to the end.
	e.resolver.cnames[target(video)] = "edge.amazonaws.com."
	e.resolver.cnames[video] = "edge.amazonaws.com."

	d, _ := e.m.Check(ctx, video)
	wantStatus(t, d, settings.StatusActive)
}

func TestRoute53DomainLifecycle(t *testing.T) {
	e := newEnv(t, zoneID, "Z999")
	e.certs.certs[certARN(video)] = Cert{Status: CertPendingValidation, Records: validation(video)}

	d, err := e.m.Create(ctx, CreateRequest{Domain: video, DNSMode: settings.DNSRoute53, HostedZoneID: zoneID, FallbackURL: "https://example.org/"})
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, d, settings.StatusPendingValidation)
	v := validation(video)[0]
	if e.zones.records[rkey(zoneID, v.Name, "CNAME")] != v.Value {
		t.Fatalf("validation record not written: %v", e.zones.records)
	}

	e.certs.issue(video, CertIssued)
	d, err = e.m.Check(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, d, settings.StatusActive)
	if e.zones.records[rkey(zoneID, video, "A")] != target(video) {
		t.Errorf("alias not written: %v", e.zones.records)
	}
	if len(d.DNSRecords) != 0 || d.TargetDomain != target(video) {
		t.Errorf("route53 domain = %+v", d)
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name  string
		zones []string
		req   CreateRequest
		msg   string
	}{
		{"invalid domain", nil, CreateRequest{Domain: "not a domain", DNSMode: "external", FallbackURL: "https://x.xx"}, "Domain 'not a domain' is not a valid hostname"},
		{"admin domain", nil, CreateRequest{Domain: adminDomain, DNSMode: "external", FallbackURL: "https://x.xx"}, "Domain 'admin.short.example.com' is the admin frontend's domain"},
		{"mode", nil, CreateRequest{Domain: video, DNSMode: "manual", FallbackURL: "https://x.xx"}, "dns_mode must be 'external' or 'route53'"},
		{"zone in external mode", []string{zoneID}, CreateRequest{Domain: video, DNSMode: "external", HostedZoneID: zoneID, FallbackURL: "https://x.xx"}, "hosted_zone_id is only used with dns_mode 'route53'"},
		{"route53 disabled", nil, CreateRequest{Domain: video, DNSMode: "route53", HostedZoneID: zoneID, FallbackURL: "https://x.xx"}, "Route 53 mode is disabled: the Terraform variable route53_zone_ids lists no hosted zones"},
		{"zone not allowed", []string{"Z999"}, CreateRequest{Domain: video, DNSMode: "route53", HostedZoneID: zoneID, FallbackURL: "https://x.xx"}, "Hosted zone 'Z123' is not one of the zones in route53_zone_ids: Z999"},
		{"fallback missing", nil, CreateRequest{Domain: video, DNSMode: "external"}, "fallback_url is required"},
		{"fallback http", nil, CreateRequest{Domain: video, DNSMode: "external", FallbackURL: "http://example.org"}, "fallback_url must start with https://"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.zones...)
			_, err := e.m.Create(ctx, tt.req)
			if got := wantErr(t, err, 400); got.Message != tt.msg {
				t.Errorf("message = %q, want %q", got.Message, tt.msg)
			}
			if e.certs.total() != 0 || len(e.ssm.Names()) != 1 {
				t.Error("a rejected create requested a certificate or stored a setting")
			}
		})
	}
}

func TestCreateExistingDomain(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	_, err := e.m.Create(ctx, CreateRequest{Domain: video, DNSMode: "external", FallbackURL: "https://x.xx"})
	wantErr(t, err, 409)
	_, err = e.m.Create(ctx, CreateRequest{Domain: "short.example.com", DNSMode: "external", FallbackURL: "https://x.xx"})
	wantErr(t, err, 409)
	if e.certs.n("Request") != 1 {
		t.Errorf("certificates requested = %d, want 1", e.certs.n("Request"))
	}
}

func TestCertificateFailure(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.certs.certs[certARN(video)] = Cert{Status: "FAILED", FailureReason: "CAA_ERROR"}

	d, err := e.m.Check(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, d, settings.StatusFailed)
	if d.Error != "Certificate FAILED: CAA_ERROR" || e.stored(t, video).Error != d.Error {
		t.Errorf("error = %q", d.Error)
	}

	// Failed is final until the domain is deleted.
	e.certs.issue(video, CertIssued)
	if d, _ = e.m.Check(ctx, video); d.Status != settings.StatusFailed || e.gateway.total() != 0 {
		t.Errorf("re-check of a failed domain = %+v", d)
	}
}

func TestTransientErrorsAreRecordedAndRetried(t *testing.T) {
	e := newEnv(t)
	e.certs.fail("Request", errors.New("throttled"))

	d := createExternal(t, e)
	wantStatus(t, d, settings.StatusPendingValidation)
	if d.CertARN != "" || d.Error != "requesting certificate: throttled" || e.stored(t, video).Error != d.Error {
		t.Fatalf("after failed request = %+v", d)
	}

	e.certs.fail("Request", nil)
	e.certs.issue(video, CertIssued)
	e.gateway.fail("EnsureDomain", errors.New("too many requests"))
	d, err := e.m.Check(ctx, video)
	if err != nil {
		t.Fatal(err)
	}
	// The issued certificate is kept; the custom domain step is retried.
	wantStatus(t, d, settings.StatusPendingDNS)
	if d.CertARN != certARN(video) || d.Error != "creating custom domain: too many requests" {
		t.Fatalf("after gateway failure = %+v", d)
	}

	e.gateway.fail("EnsureDomain", nil)
	d, _ = e.m.Check(ctx, video)
	wantStatus(t, d, settings.StatusPendingDNS)
	if d.Error != "" || d.TargetDomain != target(video) {
		t.Errorf("after retry = %+v", d)
	}
}

func TestCheckUnknownAndPrimary(t *testing.T) {
	e := newEnv(t)
	_, err := e.m.Check(ctx, "nope.example.com")
	wantErr(t, err, 404)
	_, err = e.m.Check(ctx, "not valid")
	wantErr(t, err, 400)

	d, err := e.m.Check(ctx, "short.example.com")
	if err != nil || d.Status != settings.StatusActive || e.certs.total()+e.gateway.total() != 0 {
		t.Errorf("check primary = %+v, %v", d, err)
	}
}

func TestCheckAll(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	if _, err := e.m.Create(ctx, CreateRequest{Domain: "other.example.org", DNSMode: "external", FallbackURL: "https://x.xx"}); err != nil {
		t.Fatal(err)
	}
	e.certs.issue(video, CertIssued)
	e.certs.issue("other.example.org", "VALIDATION_TIMED_OUT")
	e.ssm.Put(paramPath, settings.Domain{Domain: "done.example.org", Role: settings.RoleSecondary, Status: settings.StatusActive})

	if err := e.m.CheckAll(ctx); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.stored(t, video), settings.StatusPendingDNS)
	wantStatus(t, e.stored(t, "other.example.org"), settings.StatusFailed)
	wantStatus(t, e.stored(t, "done.example.org"), settings.StatusActive)
	if e.certs.n("Describe") != 4 { // two on create, two in CheckAll, none for the active domain
		t.Errorf("certificates read = %d, want 4", e.certs.n("Describe"))
	}

	// A failing domain does not stop the others; a failed store write is
	// returned.
	e.ssm.SetErr(errors.New("ssm down"))
	if err := e.m.CheckAll(ctx); err == nil {
		t.Error("CheckAll ignored a failing parameter store")
	}
}

func TestUpdateFallback(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)

	d, err := e.m.UpdateFallback(ctx, video, "https://example.org/new")
	if err != nil || d.FallbackURL != "https://example.org/new" || e.stored(t, video).FallbackURL != d.FallbackURL {
		t.Fatalf("update = %+v, %v", d, err)
	}
	_, err = e.m.UpdateFallback(ctx, video, "ftp://x")
	wantErr(t, err, 400)
	_, err = e.m.UpdateFallback(ctx, "short.example.com", "https://example.org/new")
	wantErr(t, err, 409)
	_, err = e.m.UpdateFallback(ctx, "nope.example.com", "https://example.org/new")
	wantErr(t, err, 404)
}

func TestDeleteRefusals(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.addLink(video, "a")
	e.addLink(video, "b")
	e.addLink("short.example.com", "c")

	err := e.m.Delete(ctx, "short.example.com")
	wantErr(t, err, 409)

	err = e.m.Delete(ctx, video)
	if got := wantErr(t, err, 409); got.LinkCount != 2 {
		t.Errorf("link count = %d, want 2", got.LinkCount)
	}

	wantErr(t, e.m.Delete(ctx, "nope.example.com"), 404)
	if e.gateway.n("DeleteDomain") != 0 || e.certs.n("Delete") != 0 || len(e.ssm.Names()) != 2 {
		t.Error("a refused delete removed something")
	}
}

func TestDeleteRoute53Domain(t *testing.T) {
	e := newEnv(t, zoneID)
	e.certs.issue(video, CertIssued)
	if _, err := e.m.Create(ctx, CreateRequest{Domain: video, DNSMode: "route53", HostedZoneID: zoneID, FallbackURL: "https://x.xx"}); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.stored(t, video), settings.StatusActive)
	e.zones.set(rkey(zoneID, "other.example.org", "A"), "keep")

	if err := e.m.Delete(ctx, video); err != nil {
		t.Fatal(err)
	}
	if len(e.zones.records) != 1 {
		t.Errorf("records left = %v, want only the unrelated one", e.zones.records)
	}
	if _, ok := e.gateway.domains[video]; ok {
		t.Error("custom domain left")
	}
	if !slices.Equal(e.certs.deleted, []string{certARN(video)}) {
		t.Errorf("certificates deleted = %v", e.certs.deleted)
	}
	if names := e.ssm.Names(); len(names) != 1 {
		t.Errorf("parameters left = %v", names)
	}
}

func TestDeleteWaitsForCertificateRelease(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.certs.issue(video, CertIssued)
	e.m.Check(ctx, video)
	e.certs.inUse = 2

	if err := e.m.Delete(ctx, video); err != nil {
		t.Fatal(err)
	}
	if e.sleeps != 2 || len(e.certs.deleted) != 1 || len(e.ssm.Names()) != 1 {
		t.Errorf("sleeps = %d, deleted = %v, params = %v", e.sleeps, e.certs.deleted, e.ssm.Names())
	}
	if e.gateway.total() == 0 || len(e.gateway.domains) != 0 {
		t.Error("custom domain not deleted")
	}
}

func TestDeleteCertificateStillInUseCanBeRepeated(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.certs.issue(video, CertIssued)
	e.m.Check(ctx, video)
	e.certs.inUse = -1

	wantErr(t, e.m.Delete(ctx, video), 503)
	if e.sleeps != certDeleteAttempts-1 {
		t.Errorf("sleeps = %d", e.sleeps)
	}
	if len(e.ssm.Names()) != 2 {
		t.Fatal("setting removed although the certificate is left")
	}

	e.certs.inUse = 0
	if err := e.m.Delete(ctx, video); err != nil {
		t.Fatal(err)
	}
	if len(e.certs.deleted) != 1 || len(e.ssm.Names()) != 1 {
		t.Errorf("after repeat: deleted = %v, params = %v", e.certs.deleted, e.ssm.Names())
	}
}

func TestDeleteLeavesUntaggedCertificate(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.certs.untagged[certARN(video)] = true

	if err := e.m.Delete(ctx, video); err != nil {
		t.Fatal(err)
	}
	if len(e.certs.deleted) != 0 || len(e.ssm.Names()) != 1 {
		t.Errorf("deleted = %v, params = %v", e.certs.deleted, e.ssm.Names())
	}
}

func TestRoute53RecordConflictIsStored(t *testing.T) {
	e := newEnv(t, zoneID)
	e.certs.issue(video, CertIssued)
	e.zones.fail("UpsertAlias", errors.New("A video.example.org: record exists with another value"))

	d, err := e.m.Create(ctx, CreateRequest{Domain: video, DNSMode: settings.DNSRoute53, HostedZoneID: zoneID, FallbackURL: "https://x.xx"})
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, d, settings.StatusPendingDNS)
	stored := e.stored(t, video)
	if stored.Error != "writing alias record: A video.example.org: record exists with another value" || stored.Status != settings.StatusPendingDNS {
		t.Errorf("stored = %+v", stored)
	}
}

func TestCheckDoesNotRecreateDomainDeletedMeanwhile(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.certs.issue(video, CertIssued)
	// The domain is deleted while the scheduled check creates its custom domain.
	e.gateway.onEnsure = func() {
		e.gateway.onEnsure = nil
		if err := e.m.Delete(ctx, video); err != nil {
			t.Fatal(err)
		}
	}

	if err := e.m.CheckAll(ctx); err != nil {
		t.Fatal(err)
	}
	if names := e.ssm.Names(); len(names) != 1 {
		t.Errorf("parameters = %v, want only the primary's", names)
	}
}
