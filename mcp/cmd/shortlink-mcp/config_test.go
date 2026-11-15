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
		envAPIBase:      "https://short.test",
		envRegion:       "eu-central-1",
		envClientID:     "example-client",
		envRefreshToken: "placeholder-token",
	}
}

func TestServerConfig(t *testing.T) {
	c, err := serverConfigFromEnv(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Region != "eu-central-1" || c.RefreshToken != "placeholder-token" {
		t.Fatalf("config = %+v", c)
	}
}

func TestServerConfigIgnoresUsernameAndPassword(t *testing.T) {
	m := validEnv()
	delete(m, envRefreshToken)
	m["SHORTLINK_USERNAME"] = "user@example.test"
	m["SHORTLINK_PASSWORD"] = "placeholder"
	_, err := serverConfigFromEnv(env(m))
	if err == nil || !strings.Contains(err.Error(), envRefreshToken) || !strings.Contains(err.Error(), "shortlink-mcp login") {
		t.Fatalf("err = %v", err)
	}
}

func TestServerConfigMissingNamesVariablesNotValues(t *testing.T) {
	m := validEnv()
	delete(m, envRegion)
	delete(m, envClientID)
	_, err := serverConfigFromEnv(env(m))
	if err == nil || !strings.Contains(err.Error(), envRegion+", "+envClientID) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "placeholder-token") {
		t.Fatal("error leaks the refresh token")
	}
}

func TestServerConfigBadAPIBase(t *testing.T) {
	m := validEnv()
	m[envAPIBase] = "short.test"
	if _, err := serverConfigFromEnv(env(m)); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoginConfigValidate(t *testing.T) {
	if err := (loginConfig{Region: "eu-central-1"}).validate(); err == nil || !strings.Contains(err.Error(), envClientID) {
		t.Fatalf("err = %v", err)
	}
	if err := (loginConfig{Region: "eu-central-1", ClientID: "c"}).validate(); err != nil {
		t.Fatal(err)
	}
}
