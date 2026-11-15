package settings_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings/settingstest"
)

const path = "/stack/domains"

func TestCreateGetPutDelete(t *testing.T) {
	ctx := context.Background()
	fake := settingstest.New()
	s := fake.Store(path)

	d := settings.Domain{Domain: "go.example.com", Role: settings.RoleSecondary, Status: settings.StatusPendingValidation}
	if err := s.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, d); !errors.Is(err, settings.ErrExists) {
		t.Fatalf("second Create = %v, want ErrExists", err)
	}
	if got := fake.Names(); len(got) != 1 || got[0] != "/stack/domains/go.example.com" {
		t.Fatalf("names = %v", got)
	}

	got, ok, err := s.Get(ctx, "go.example.com")
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if got.ValidationRecords == nil || got.DNSRecords == nil {
		t.Error("record lists are stored as null, want []")
	}

	d.Status = settings.StatusActive
	if err := s.Put(ctx, d); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Get(ctx, "go.example.com"); got.Status != settings.StatusActive {
		t.Errorf("status after Put = %q", got.Status)
	}

	if err := s.Delete(ctx, "go.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "go.example.com"); !errors.Is(err, settings.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	if _, ok, err := s.Get(ctx, "go.example.com"); ok || err != nil {
		t.Errorf("Get after Delete = %v, %v", ok, err)
	}
}

func TestListOrdersPrimaryFirstAndPages(t *testing.T) {
	fake := settingstest.New()
	for i := range 23 {
		fake.Put(path, settings.Domain{Domain: fmt.Sprintf("d%02d.example.com", i), Role: settings.RoleSecondary})
	}
	fake.Put(path, settings.Domain{Domain: "zz.example.com", Role: settings.RolePrimary})
	fake.SetRaw("/stack/domains/nested/x.example.com", `{"domain":"x.example.com"}`)
	fake.SetRaw("/other/domains/y.example.com", `{"domain":"y.example.com"}`)

	got, err := fake.Store(path + "/").List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 24 {
		t.Fatalf("len = %d, want 24", len(got))
	}
	if got[0].Domain != "zz.example.com" || got[1].Domain != "d00.example.com" || got[23].Domain != "d22.example.com" {
		t.Errorf("order = %s, %s, ..., %s", got[0].Domain, got[1].Domain, got[23].Domain)
	}
	if n := fake.Count("GetParametersByPath"); n != 3 {
		t.Errorf("pages read = %d, want 3", n)
	}
}

func TestListFailsOnInvalidJSON(t *testing.T) {
	fake := settingstest.New()
	fake.SetRaw("/stack/domains/go.example.com", "not json")
	if _, err := fake.Store(path).List(context.Background()); err == nil {
		t.Fatal("List succeeded on an invalid parameter")
	}
}
