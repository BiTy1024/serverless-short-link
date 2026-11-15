package main

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func validEnv() map[string]string {
	return map[string]string{
		"SHORTLINK_API_BASE":             "https://short.test",
		"SHORTLINK_COGNITO_USER_POOL_ID": "eu-central-1_Example",
		"SHORTLINK_COGNITO_CLIENT_ID":    "example-client",
		"SHORTLINK_USERNAME":             "user@example.test",
		"SHORTLINK_PASSWORD":             "placeholder",
	}
}

func TestConfigDerivesRegion(t *testing.T) {
	c, err := configFromEnv(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Region != "eu-central-1" {
		t.Fatalf("region = %q", c.Region)
	}
}

func TestConfigMissingNamesVariablesNotValues(t *testing.T) {
	m := validEnv()
	delete(m, "SHORTLINK_USERNAME")
	delete(m, "SHORTLINK_COGNITO_CLIENT_ID")
	_, err := configFromEnv(env(m))
	if err == nil || !strings.Contains(err.Error(), "SHORTLINK_COGNITO_CLIENT_ID, SHORTLINK_USERNAME") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "placeholder") {
		t.Fatal("error leaks the password")
	}
}

func TestConfigRegionMismatch(t *testing.T) {
	m := validEnv()
	m["SHORTLINK_COGNITO_REGION"] = "us-east-1"
	if _, err := configFromEnv(env(m)); err == nil {
		t.Fatal("expected error")
	}
}

func TestConfigBadAPIBase(t *testing.T) {
	m := validEnv()
	m["SHORTLINK_API_BASE"] = "short.test"
	if _, err := configFromEnv(env(m)); err == nil {
		t.Fatal("expected error")
	}
}
