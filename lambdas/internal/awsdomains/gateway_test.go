package awsdomains

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	gwtypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

const (
	ourAPI   = "api-ours"
	otherAPI = "api-other"
	stack    = "stack"
	host     = "video.example.org"
)

var ctx = context.Background()

type fakeDomain struct {
	tags     map[string]string
	mappings []gwtypes.ApiMapping
}

// fakeGateway keeps custom domains and their API mappings.
type fakeGateway struct {
	domains map[string]*fakeDomain
	n       int
	// onCreateMapping, when set, runs instead of CreateApiMapping and
	// answers with a ConflictException, as when a mapping appears concurrently.
	onCreateMapping func(d *fakeDomain)
	deletedDomains  []string
}

func newFakeGateway() *fakeGateway { return &fakeGateway{domains: map[string]*fakeDomain{}} }

func (f *fakeGateway) add(name string, tags map[string]string, apis ...string) {
	d := &fakeDomain{tags: tags}
	for _, api := range apis {
		d.mapping(f, api)
	}
	f.domains[name] = d
}

func (d *fakeDomain) mapping(f *fakeGateway, api string) {
	f.n++
	d.mappings = append(d.mappings, gwtypes.ApiMapping{ApiId: aws.String(api), ApiMappingId: aws.String(fmt.Sprint("m", f.n)), Stage: aws.String("$default")})
}

func (f *fakeGateway) apis(name string) []string {
	var out []string
	for _, m := range f.domains[name].mappings {
		out = append(out, aws.ToString(m.ApiId))
	}
	return out
}

func (f *fakeGateway) GetDomainName(_ context.Context, in *apigatewayv2.GetDomainNameInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.GetDomainNameOutput, error) {
	d, ok := f.domains[aws.ToString(in.DomainName)]
	if !ok {
		return nil, &gwtypes.NotFoundException{}
	}
	return &apigatewayv2.GetDomainNameOutput{
		DomainName: in.DomainName,
		DomainNameConfigurations: []gwtypes.DomainNameConfiguration{{
			ApiGatewayDomainName: aws.String("d-1.execute-api.eu-central-1.amazonaws.com"),
			HostedZoneId:         aws.String("ZGW"),
		}},
		Tags: d.tags,
	}, nil
}

func (f *fakeGateway) CreateDomainName(_ context.Context, in *apigatewayv2.CreateDomainNameInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.CreateDomainNameOutput, error) {
	name := aws.ToString(in.DomainName)
	if _, ok := f.domains[name]; ok {
		return nil, &gwtypes.ConflictException{}
	}
	f.domains[name] = &fakeDomain{tags: in.Tags}
	return &apigatewayv2.CreateDomainNameOutput{}, nil
}

func (f *fakeGateway) DeleteDomainName(_ context.Context, in *apigatewayv2.DeleteDomainNameInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.DeleteDomainNameOutput, error) {
	name := aws.ToString(in.DomainName)
	if _, ok := f.domains[name]; !ok {
		return nil, &gwtypes.NotFoundException{}
	}
	delete(f.domains, name)
	f.deletedDomains = append(f.deletedDomains, name)
	return &apigatewayv2.DeleteDomainNameOutput{}, nil
}

func (f *fakeGateway) GetApiMappings(_ context.Context, in *apigatewayv2.GetApiMappingsInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.GetApiMappingsOutput, error) {
	d, ok := f.domains[aws.ToString(in.DomainName)]
	if !ok {
		return nil, &gwtypes.NotFoundException{}
	}
	// One mapping per page, so paging is exercised.
	start := 0
	if in.NextToken != nil {
		fmt.Sscan(aws.ToString(in.NextToken), &start)
	}
	out := &apigatewayv2.GetApiMappingsOutput{}
	if start < len(d.mappings) {
		out.Items = d.mappings[start : start+1]
	}
	if start+1 < len(d.mappings) {
		out.NextToken = aws.String(fmt.Sprint(start + 1))
	}
	return out, nil
}

func (f *fakeGateway) CreateApiMapping(_ context.Context, in *apigatewayv2.CreateApiMappingInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.CreateApiMappingOutput, error) {
	d := f.domains[aws.ToString(in.DomainName)]
	if f.onCreateMapping != nil {
		f.onCreateMapping(d)
		return nil, &gwtypes.ConflictException{}
	}
	d.mapping(f, aws.ToString(in.ApiId))
	return &apigatewayv2.CreateApiMappingOutput{}, nil
}

func (f *fakeGateway) DeleteApiMapping(_ context.Context, in *apigatewayv2.DeleteApiMappingInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.DeleteApiMappingOutput, error) {
	d := f.domains[aws.ToString(in.DomainName)]
	d.mappings = slices.DeleteFunc(d.mappings, func(m gwtypes.ApiMapping) bool {
		return aws.ToString(m.ApiMappingId) == aws.ToString(in.ApiMappingId)
	})
	return &apigatewayv2.DeleteApiMappingOutput{}, nil
}

func gateway(f *fakeGateway) *Gateway {
	return &Gateway{Client: f, APIID: ourAPI, Stage: "$default", Stack: stack}
}

var ours = map[string]string{StackTag: stack}

func TestEnsureDomainCreatesTaggedDomainAndMapping(t *testing.T) {
	f := newFakeGateway()
	g := gateway(f)
	for range 2 {
		ep, err := g.EnsureDomain(ctx, host, "arn:cert")
		if err != nil || ep.Target == "" {
			t.Fatalf("EnsureDomain = %+v, %v", ep, err)
		}
	}
	if f.domains[host].tags[StackTag] != stack {
		t.Errorf("tags = %v", f.domains[host].tags)
	}
	if got := f.apis(host); !slices.Equal(got, []string{ourAPI}) {
		t.Errorf("mappings = %v, want one to our API", got)
	}
}

func TestEnsureDomainReusesUntaggedDomainWithoutForeignMappings(t *testing.T) {
	for _, apis := range [][]string{nil, {ourAPI}} {
		f := newFakeGateway()
		f.add(host, nil, apis...)
		if _, err := gateway(f).EnsureDomain(ctx, host, "arn:cert"); err != nil {
			t.Fatalf("mappings %v: %v", apis, err)
		}
		if got := f.apis(host); !slices.Equal(got, []string{ourAPI}) {
			t.Errorf("mappings %v: after = %v", apis, got)
		}
	}
}

func TestEnsureDomainRefusesForeignDomain(t *testing.T) {
	for _, tags := range []map[string]string{nil, {StackTag: "other-stack"}} {
		f := newFakeGateway()
		f.add(host, tags, otherAPI)
		_, err := gateway(f).EnsureDomain(ctx, host, "arn:cert")
		if !errors.Is(err, ErrForeignDomain) {
			t.Fatalf("tags %v: err = %v, want ErrForeignDomain", tags, err)
		}
		if got := f.apis(host); !slices.Equal(got, []string{otherAPI}) {
			t.Errorf("tags %v: mappings changed to %v", tags, got)
		}
	}
}

func TestEnsureMappingRefusesForeignMappingOnOwnDomain(t *testing.T) {
	f := newFakeGateway()
	f.add(host, ours, otherAPI)
	if _, err := gateway(f).EnsureDomain(ctx, host, "arn:cert"); !errors.Is(err, ErrForeignDomain) {
		t.Fatalf("err = %v, want ErrForeignDomain", err)
	}
}

func TestEnsureMappingConflict(t *testing.T) {
	f := newFakeGateway()
	f.add(host, ours)
	f.onCreateMapping = func(d *fakeDomain) { d.mapping(f, ourAPI) }
	if _, err := gateway(f).EnsureDomain(ctx, host, "arn:cert"); err != nil {
		t.Fatalf("conflict with our mapping: %v", err)
	}

	f = newFakeGateway()
	f.add(host, ours)
	f.onCreateMapping = func(d *fakeDomain) { d.mapping(f, otherAPI) }
	if _, err := gateway(f).EnsureDomain(ctx, host, "arn:cert"); !errors.Is(err, ErrForeignDomain) {
		t.Fatalf("conflict with a foreign mapping: err = %v, want ErrForeignDomain", err)
	}
}

func TestDeleteDomain(t *testing.T) {
	tests := []struct {
		name       string
		tags       map[string]string
		apis       []string
		wantLeft   []string
		wantExists bool
	}{
		{"own domain, own mapping", ours, []string{ourAPI}, nil, false},
		{"own domain, foreign mapping too", ours, []string{ourAPI, otherAPI}, []string{otherAPI}, true},
		{"untagged domain", nil, []string{ourAPI}, nil, true},
		{"other stack's domain", map[string]string{StackTag: "other"}, []string{otherAPI}, []string{otherAPI}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeGateway()
			f.add(host, tt.tags, tt.apis...)
			if err := gateway(f).DeleteDomain(ctx, host); err != nil {
				t.Fatal(err)
			}
			_, exists := f.domains[host]
			if exists != tt.wantExists {
				t.Fatalf("domain exists = %v, want %v", exists, tt.wantExists)
			}
			if exists && !slices.Equal(f.apis(host), tt.wantLeft) {
				t.Errorf("mappings left = %v, want %v", f.apis(host), tt.wantLeft)
			}
		})
	}

	f := newFakeGateway()
	if err := gateway(f).DeleteDomain(ctx, host); err != nil {
		t.Errorf("missing domain: %v", err)
	}
}
