// Package awsdomains implements the AWS side of the domains package: ACM
// certificates, API Gateway custom domains and Route 53 records. The
// adapters are thin; the decisions live in package domains.
package awsdomains

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	gwtypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domains"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
)

// StackTag is the tag key that marks a certificate as requested by a stack;
// its value is the stack name. Only certificates tagged with this stack's
// name are deleted, and the IAM policy enforces the same.
const StackTag = "short-link-stack"

// Certs implements domains.Certs with ACM in the Lambda's region.
type Certs struct {
	Client *acm.Client
	Stack  string
}

// Request asks for a DNS-validated certificate. The idempotency token is
// derived from the domain, so a retried request within an hour returns the
// same certificate instead of a second one.
func (c *Certs) Request(ctx context.Context, domain string) (string, error) {
	sum := sha256.Sum256([]byte(c.Stack + "/" + domain))
	out, err := c.Client.RequestCertificate(ctx, &acm.RequestCertificateInput{
		DomainName:       aws.String(domain),
		ValidationMethod: acmtypes.ValidationMethodDns,
		IdempotencyToken: aws.String(hex.EncodeToString(sum[:16])),
		Tags:             []acmtypes.Tag{{Key: aws.String(StackTag), Value: aws.String(c.Stack)}},
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.CertificateArn), nil
}

// Describe returns the certificate's status and validation records.
func (c *Certs) Describe(ctx context.Context, arn string) (domains.Cert, error) {
	out, err := c.Client.DescribeCertificate(ctx, &acm.DescribeCertificateInput{CertificateArn: aws.String(arn)})
	if err != nil {
		return domains.Cert{}, err
	}
	cert := domains.Cert{
		Status:        string(out.Certificate.Status),
		FailureReason: string(out.Certificate.FailureReason),
	}
	for _, o := range out.Certificate.DomainValidationOptions {
		if r := o.ResourceRecord; r != nil {
			cert.Records = append(cert.Records, settings.Record{
				Name:  aws.ToString(r.Name),
				Type:  string(r.Type),
				Value: aws.ToString(r.Value),
			})
		}
	}
	return cert, nil
}

// Delete removes the certificate when it carries StackTag with this stack's
// name.
func (c *Certs) Delete(ctx context.Context, arn string) (bool, error) {
	var notFound *acmtypes.ResourceNotFoundException
	tags, err := c.Client.ListTagsForCertificate(ctx, &acm.ListTagsForCertificateInput{CertificateArn: aws.String(arn)})
	if errors.As(err, &notFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	tagged := false
	for _, t := range tags.Tags {
		if aws.ToString(t.Key) == StackTag && aws.ToString(t.Value) == c.Stack {
			tagged = true
		}
	}
	if !tagged {
		return false, nil
	}
	_, err = c.Client.DeleteCertificate(ctx, &acm.DeleteCertificateInput{CertificateArn: aws.String(arn)})
	var inUse *acmtypes.ResourceInUseException
	switch {
	case errors.As(err, &inUse):
		return false, domains.ErrCertInUse
	case errors.As(err, &notFound):
		return true, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// Gateway implements domains.Gateway with API Gateway v2 custom domains
// mapped to one stage of one HTTP API.
type Gateway struct {
	Client *apigatewayv2.Client
	APIID  string
	Stage  string
}

// FindDomain returns the regional endpoint of the custom domain.
func (g *Gateway) FindDomain(ctx context.Context, domain string) (domains.Endpoint, bool, error) {
	out, err := g.Client.GetDomainName(ctx, &apigatewayv2.GetDomainNameInput{DomainName: aws.String(domain)})
	var notFound *gwtypes.NotFoundException
	if errors.As(err, &notFound) {
		return domains.Endpoint{}, false, nil
	}
	if err != nil {
		return domains.Endpoint{}, false, err
	}
	if len(out.DomainNameConfigurations) == 0 {
		return domains.Endpoint{}, false, errors.New("custom domain " + domain + " has no configuration")
	}
	cfg := out.DomainNameConfigurations[0]
	return domains.Endpoint{
		Target:       aws.ToString(cfg.ApiGatewayDomainName),
		HostedZoneID: aws.ToString(cfg.HostedZoneId),
	}, true, nil
}

// EnsureDomain creates the regional custom domain (TLS 1.2) and its mapping
// to the API's stage unless they exist.
func (g *Gateway) EnsureDomain(ctx context.Context, domain, certARN string) (domains.Endpoint, error) {
	ep, found, err := g.FindDomain(ctx, domain)
	if err != nil {
		return domains.Endpoint{}, err
	}
	if !found {
		_, err := g.Client.CreateDomainName(ctx, &apigatewayv2.CreateDomainNameInput{
			DomainName: aws.String(domain),
			DomainNameConfigurations: []gwtypes.DomainNameConfiguration{{
				CertificateArn: aws.String(certARN),
				EndpointType:   gwtypes.EndpointTypeRegional,
				SecurityPolicy: gwtypes.SecurityPolicyTls12,
			}},
		})
		var conflict *gwtypes.ConflictException
		if err != nil && !errors.As(err, &conflict) {
			return domains.Endpoint{}, err
		}
		if ep, found, err = g.FindDomain(ctx, domain); err != nil {
			return domains.Endpoint{}, err
		} else if !found {
			return domains.Endpoint{}, errors.New("custom domain " + domain + " not found after creating it")
		}
	}
	return ep, g.ensureMapping(ctx, domain)
}

func (g *Gateway) ensureMapping(ctx context.Context, domain string) error {
	out, err := g.Client.GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{DomainName: aws.String(domain)})
	if err != nil {
		return err
	}
	for _, m := range out.Items {
		if aws.ToString(m.ApiId) == g.APIID {
			return nil
		}
	}
	_, err = g.Client.CreateApiMapping(ctx, &apigatewayv2.CreateApiMappingInput{
		ApiId:      aws.String(g.APIID),
		DomainName: aws.String(domain),
		Stage:      aws.String(g.Stage),
	})
	var conflict *gwtypes.ConflictException
	if errors.As(err, &conflict) {
		return nil
	}
	return err
}

// DeleteDomain removes every API mapping of the custom domain, then the
// custom domain.
func (g *Gateway) DeleteDomain(ctx context.Context, domain string) error {
	var notFound *gwtypes.NotFoundException
	out, err := g.Client.GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{DomainName: aws.String(domain)})
	if errors.As(err, &notFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, m := range out.Items {
		_, err := g.Client.DeleteApiMapping(ctx, &apigatewayv2.DeleteApiMappingInput{
			ApiMappingId: m.ApiMappingId,
			DomainName:   aws.String(domain),
		})
		if err != nil && !errors.As(err, &notFound) {
			return err
		}
	}
	_, err = g.Client.DeleteDomainName(ctx, &apigatewayv2.DeleteDomainNameInput{DomainName: aws.String(domain)})
	if errors.As(err, &notFound) {
		return nil
	}
	return err
}

// Zones implements domains.Zones with Route 53.
type Zones struct {
	Client *route53.Client
}

// validationTTL is the TTL of the certificate validation CNAMEs.
const validationTTL = 300

func (z *Zones) change(ctx context.Context, zoneID string, action r53types.ChangeAction, set *r53types.ResourceRecordSet) error {
	_, err := z.Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch: &r53types.ChangeBatch{
			Changes: []r53types.Change{{Action: action, ResourceRecordSet: set}},
		},
	})
	return err
}

// UpsertCNAME writes a CNAME record.
func (z *Zones) UpsertCNAME(ctx context.Context, zoneID string, r settings.Record) error {
	return z.change(ctx, zoneID, r53types.ChangeActionUpsert, &r53types.ResourceRecordSet{
		Name:            aws.String(r.Name),
		Type:            r53types.RRType(r.Type),
		TTL:             aws.Int64(validationTTL),
		ResourceRecords: []r53types.ResourceRecord{{Value: aws.String(r.Value)}},
	})
}

// UpsertAlias writes an A alias record for name pointing at the custom
// domain's regional endpoint.
func (z *Zones) UpsertAlias(ctx context.Context, zoneID, name string, target domains.Endpoint) error {
	return z.change(ctx, zoneID, r53types.ChangeActionUpsert, &r53types.ResourceRecordSet{
		Name: aws.String(name),
		Type: r53types.RRTypeA,
		AliasTarget: &r53types.AliasTarget{
			DNSName:              aws.String(target.Target),
			HostedZoneId:         aws.String(target.HostedZoneID),
			EvaluateTargetHealth: false,
		},
	})
}

// DeleteCNAME removes the CNAME record r when it still holds r.Value.
func (z *Zones) DeleteCNAME(ctx context.Context, zoneID string, r settings.Record) error {
	set, err := z.find(ctx, zoneID, r.Name, r53types.RRType(r.Type))
	if err != nil || set == nil {
		return err
	}
	if len(set.ResourceRecords) != 1 || canonical(aws.ToString(set.ResourceRecords[0].Value)) != canonical(r.Value) {
		return nil
	}
	return z.change(ctx, zoneID, r53types.ChangeActionDelete, set)
}

// DeleteAlias removes the A alias record of name when it still points at
// target.
func (z *Zones) DeleteAlias(ctx context.Context, zoneID, name string, target domains.Endpoint) error {
	set, err := z.find(ctx, zoneID, name, r53types.RRTypeA)
	if err != nil || set == nil {
		return err
	}
	if set.AliasTarget == nil || canonical(aws.ToString(set.AliasTarget.DNSName)) != canonical(target.Target) {
		return nil
	}
	return z.change(ctx, zoneID, r53types.ChangeActionDelete, set)
}

// find returns the record set of name and type, or nil when there is none.
// Route 53 deletes only exact matches, so the stored set is what Delete sends.
func (z *Zones) find(ctx context.Context, zoneID, name string, typ r53types.RRType) (*r53types.ResourceRecordSet, error) {
	out, err := z.Client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(zoneID),
		StartRecordName: aws.String(name),
		StartRecordType: typ,
		MaxItems:        aws.Int32(1),
	})
	if err != nil {
		return nil, err
	}
	if len(out.ResourceRecordSets) == 0 {
		return nil, nil
	}
	set := out.ResourceRecordSets[0]
	if canonical(aws.ToString(set.Name)) != canonical(name) || set.Type != typ {
		return nil, nil
	}
	return &set, nil
}

func canonical(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}
