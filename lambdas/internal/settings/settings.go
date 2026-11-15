// Package settings holds the per-domain settings of the stack and stores them
// in SSM Parameter Store, one String parameter per domain at
// <path>/<domain> whose value is the JSON encoding of Domain.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// Roles of a domain.
const (
	RolePrimary   = "primary"
	RoleSecondary = "secondary"
)

// DNS modes: records are set by hand at a third-party DNS host, or written
// to a Route 53 hosted zone.
const (
	DNSExternal = "external"
	DNSRoute53  = "route53"
)

// Statuses of a domain. Only an active domain is served by the redirect.
const (
	StatusPendingValidation = "pending_validation"
	StatusPendingDNS        = "pending_dns"
	StatusActive            = "active"
	StatusFailed            = "failed"
)

// Record is one DNS record a domain needs.
type Record struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Domain is the stored setting of one domain.
type Domain struct {
	Domain       string `json:"domain"`
	Role         string `json:"role"`
	DNSMode      string `json:"dns_mode"`
	HostedZoneID string `json:"hosted_zone_id,omitempty"`
	FallbackURL  string `json:"fallback_url"`
	Status       string `json:"status"`
	CertARN      string `json:"cert_arn,omitempty"`
	TargetDomain string `json:"target_domain,omitempty"`
	// ValidationRecords are the CNAMEs ACM checks before it issues the
	// certificate.
	ValidationRecords []Record `json:"validation_records"`
	// DNSRecords are the records that point the domain at the service. They
	// are set by hand in external mode and empty in route53 mode.
	DNSRecords []Record `json:"dns_records"`
	Error      string   `json:"error,omitempty"`
	UpdatedAt  string   `json:"updated_at,omitempty"`
}

// SSMAPI is the part of the SSM client the store uses.
type SSMAPI interface {
	GetParameter(ctx context.Context, in *ssm.GetParameterInput, opts ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	PutParameter(ctx context.Context, in *ssm.PutParameterInput, opts ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	DeleteParameter(ctx context.Context, in *ssm.DeleteParameterInput, opts ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
	GetParametersByPath(ctx context.Context, in *ssm.GetParametersByPathInput, opts ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

var (
	// ErrExists is returned by Create when the domain already has a parameter.
	ErrExists = errors.New("domain already exists")
	// ErrNotFound is returned by Delete when the domain has no parameter.
	ErrNotFound = errors.New("domain not found")
)

// Store reads and writes domain settings below Path, e.g. "/stack/domains".
type Store struct {
	Client SSMAPI
	Path   string
}

func (s *Store) name(domain string) string {
	return strings.TrimSuffix(s.Path, "/") + "/" + domain
}

func decode(p types.Parameter) (Domain, error) {
	var d Domain
	if err := json.Unmarshal([]byte(aws.ToString(p.Value)), &d); err != nil {
		return Domain{}, fmt.Errorf("parameter %s: %w", aws.ToString(p.Name), err)
	}
	return d, nil
}

// List returns every stored domain: the primary first, then by name. A
// parameter that is not valid JSON fails the whole call.
func (s *Store) List(ctx context.Context) ([]Domain, error) {
	domains := []Domain{}
	p := ssm.NewGetParametersByPathPaginator(s.Client, &ssm.GetParametersByPathInput{
		Path:      aws.String(strings.TrimSuffix(s.Path, "/")),
		Recursive: aws.Bool(false),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, param := range page.Parameters {
			d, err := decode(param)
			if err != nil {
				return nil, err
			}
			domains = append(domains, d)
		}
	}
	Sort(domains)
	return domains, nil
}

// Sort orders domains with the primary first, then by name.
func Sort(domains []Domain) {
	slices.SortFunc(domains, func(a, b Domain) int {
		if (a.Role == RolePrimary) != (b.Role == RolePrimary) {
			if a.Role == RolePrimary {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Domain, b.Domain)
	})
}

// Get returns the setting of domain; ok is false when it has no parameter.
func (s *Store) Get(ctx context.Context, domain string) (d Domain, ok bool, err error) {
	out, err := s.Client.GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String(s.name(domain))})
	var nf *types.ParameterNotFound
	if errors.As(err, &nf) {
		return Domain{}, false, nil
	}
	if err != nil {
		return Domain{}, false, err
	}
	d, err = decode(*out.Parameter)
	if err != nil {
		return Domain{}, false, err
	}
	return d, true, nil
}

// Exists reports whether domain has a parameter.
func (s *Store) Exists(ctx context.Context, domain string) (bool, error) {
	_, ok, err := s.Get(ctx, domain)
	return ok, err
}

// Create stores d unless its domain already has a parameter (ErrExists).
func (s *Store) Create(ctx context.Context, d Domain) error {
	err := s.put(ctx, d, false)
	var exists *types.ParameterAlreadyExists
	if errors.As(err, &exists) {
		return ErrExists
	}
	return err
}

// Put stores d, replacing any previous value.
func (s *Store) Put(ctx context.Context, d Domain) error {
	return s.put(ctx, d, true)
}

func (s *Store) put(ctx context.Context, d Domain, overwrite bool) error {
	if d.ValidationRecords == nil {
		d.ValidationRecords = []Record{}
	}
	if d.DNSRecords == nil {
		d.DNSRecords = []Record{}
	}
	value, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.Client.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(s.name(d.Domain)),
		Value:     aws.String(string(value)),
		Type:      types.ParameterTypeString,
		Tier:      types.ParameterTierStandard,
		Overwrite: aws.Bool(overwrite),
	})
	return err
}

// Delete removes the parameter of domain (ErrNotFound when there is none).
func (s *Store) Delete(ctx context.Context, domain string) error {
	_, err := s.Client.DeleteParameter(ctx, &ssm.DeleteParameterInput{Name: aws.String(s.name(domain))})
	var nf *types.ParameterNotFound
	if errors.As(err, &nf) {
		return ErrNotFound
	}
	return err
}
