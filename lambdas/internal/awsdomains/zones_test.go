package awsdomains

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domains"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
)

// fakeZone keeps the record sets of one hosted zone. Like Route 53, a list
// starting at a name and type returns the first set at or after it, which
// may be a different record.
type fakeZone struct {
	sets    []r53types.ResourceRecordSet
	changes int
}

func setKey(name string, typ r53types.RRType) string {
	return canonical(name) + "|" + string(typ)
}

func (f *fakeZone) put(set r53types.ResourceRecordSet) {
	f.sets = slices.DeleteFunc(f.sets, func(s r53types.ResourceRecordSet) bool {
		return setKey(aws.ToString(s.Name), s.Type) == setKey(aws.ToString(set.Name), set.Type)
	})
	f.sets = append(f.sets, set)
	slices.SortFunc(f.sets, func(a, b r53types.ResourceRecordSet) int {
		return strings.Compare(setKey(aws.ToString(a.Name), a.Type), setKey(aws.ToString(b.Name), b.Type))
	})
}

func (f *fakeZone) get(name string, typ r53types.RRType) *r53types.ResourceRecordSet {
	for i, s := range f.sets {
		if setKey(aws.ToString(s.Name), s.Type) == setKey(name, typ) {
			return &f.sets[i]
		}
	}
	return nil
}

func (f *fakeZone) ChangeResourceRecordSets(_ context.Context, in *route53.ChangeResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error) {
	for _, c := range in.ChangeBatch.Changes {
		f.changes++
		set := *c.ResourceRecordSet
		switch c.Action {
		case r53types.ChangeActionUpsert:
			f.put(set)
		case r53types.ChangeActionDelete:
			got := f.get(aws.ToString(set.Name), set.Type)
			if got == nil {
				return nil, &r53types.InvalidChangeBatch{}
			}
			key := setKey(aws.ToString(set.Name), set.Type)
			f.sets = slices.DeleteFunc(f.sets, func(s r53types.ResourceRecordSet) bool {
				return setKey(aws.ToString(s.Name), s.Type) == key
			})
		}
	}
	return &route53.ChangeResourceRecordSetsOutput{}, nil
}

func (f *fakeZone) ListResourceRecordSets(_ context.Context, in *route53.ListResourceRecordSetsInput, _ ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error) {
	start := setKey(aws.ToString(in.StartRecordName), in.StartRecordType)
	for _, s := range f.sets {
		if setKey(aws.ToString(s.Name), s.Type) >= start {
			return &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []r53types.ResourceRecordSet{s}}, nil
		}
	}
	return &route53.ListResourceRecordSetsOutput{}, nil
}

func cnameSet(name, value string) r53types.ResourceRecordSet {
	return r53types.ResourceRecordSet{
		Name: aws.String(name + "."), Type: r53types.RRTypeCname, TTL: aws.Int64(300),
		ResourceRecords: []r53types.ResourceRecord{{Value: aws.String(value)}},
	}
}

func aliasSet(name, target string) r53types.ResourceRecordSet {
	return r53types.ResourceRecordSet{
		Name: aws.String(name + "."), Type: r53types.RRTypeA,
		AliasTarget: &r53types.AliasTarget{DNSName: aws.String(target + "."), HostedZoneId: aws.String("ZGW")},
	}
}

var (
	validationRecord = settings.Record{Name: "_abc.video.example.org.", Type: "CNAME", Value: "_xyz.acm-validations.aws."}
	ourTarget        = domains.Endpoint{Target: "d-1.execute-api.eu-central-1.amazonaws.com", HostedZoneID: "ZGW"}
)

func TestUpsertCNAME(t *testing.T) {
	f := &fakeZone{}
	z := &Zones{Client: f}
	for range 2 {
		if err := z.UpsertCNAME(ctx, "Z1", validationRecord); err != nil {
			t.Fatal(err)
		}
	}
	if f.changes != 1 || f.get(validationRecord.Name, r53types.RRTypeCname) == nil {
		t.Errorf("changes = %d, sets = %v", f.changes, f.sets)
	}

	f = &fakeZone{}
	f.put(cnameSet("_abc.video.example.org", "someone-else.example."))
	err := (&Zones{Client: f}).UpsertCNAME(ctx, "Z1", validationRecord)
	if !errors.Is(err, ErrRecordConflict) || f.changes != 0 {
		t.Errorf("foreign CNAME: err = %v, changes = %d", err, f.changes)
	}
}

func TestUpsertAlias(t *testing.T) {
	f := &fakeZone{}
	z := &Zones{Client: f}
	for range 2 {
		if err := z.UpsertAlias(ctx, "Z1", host, ourTarget); err != nil {
			t.Fatal(err)
		}
	}
	if f.changes != 1 {
		t.Errorf("changes = %d, want 1", f.changes)
	}

	plainA := r53types.ResourceRecordSet{
		Name: aws.String(host + "."), Type: r53types.RRTypeA, TTL: aws.Int64(300),
		ResourceRecords: []r53types.ResourceRecord{{Value: aws.String("192.0.2.1")}},
	}
	for name, existing := range map[string]r53types.ResourceRecordSet{
		"plain A":         plainA,
		"alias elsewhere": aliasSet(host, "other.cloudfront.net"),
	} {
		f := &fakeZone{}
		f.put(existing)
		err := (&Zones{Client: f}).UpsertAlias(ctx, "Z1", host, ourTarget)
		if !errors.Is(err, ErrRecordConflict) || f.changes != 0 {
			t.Errorf("%s: err = %v, changes = %d", name, err, f.changes)
		}
	}
}

func TestDeleteOnlyOwnRecords(t *testing.T) {
	f := &fakeZone{}
	f.put(cnameSet("_abc.video.example.org", validationRecord.Value))
	f.put(aliasSet(host, ourTarget.Target))
	z := &Zones{Client: f}
	if err := z.DeleteCNAME(ctx, "Z1", validationRecord); err != nil {
		t.Fatal(err)
	}
	if err := z.DeleteAlias(ctx, "Z1", host, ourTarget); err != nil {
		t.Fatal(err)
	}
	if len(f.sets) != 0 {
		t.Errorf("left = %v", f.sets)
	}

	f = &fakeZone{}
	f.put(cnameSet("_abc.video.example.org", "someone-else.example."))
	f.put(aliasSet(host, "other.cloudfront.net"))
	f.put(cnameSet("zzz.example.org", "neighbour.example."))
	z = &Zones{Client: f}
	if err := z.DeleteCNAME(ctx, "Z1", validationRecord); err != nil {
		t.Fatal(err)
	}
	if err := z.DeleteAlias(ctx, "Z1", host, ourTarget); err != nil {
		t.Fatal(err)
	}
	// A missing record: the list returns the next set, which stays.
	if err := z.DeleteCNAME(ctx, "Z1", settings.Record{Name: "missing.example.org.", Type: "CNAME", Value: "neighbour.example."}); err != nil {
		t.Fatal(err)
	}
	if len(f.sets) != 3 || f.changes != 0 {
		t.Errorf("foreign records changed: %d left, %d changes", len(f.sets), f.changes)
	}
}
