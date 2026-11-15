package main

import (
	"fmt"
	"net/url"
	"strings"
)

// config is read from these environment variables:
//
//	SHORTLINK_API_BASE              base URL of the API, e.g. https://go.example.com
//	SHORTLINK_COGNITO_USER_POOL_ID  Cognito user pool ID, <region>_<id>
//	SHORTLINK_COGNITO_CLIENT_ID     Cognito app client ID (the web client)
//	SHORTLINK_COGNITO_REGION        optional; defaults to the user pool ID's region
//	SHORTLINK_USERNAME              Cognito username (the user's email)
//	SHORTLINK_PASSWORD              Cognito password
type config struct {
	APIBase    string
	UserPoolID string
	ClientID   string
	Region     string
	Username   string
	Password   string
}

func configFromEnv(getenv func(string) string) (config, error) {
	c := config{
		APIBase:    strings.TrimSpace(getenv("SHORTLINK_API_BASE")),
		UserPoolID: strings.TrimSpace(getenv("SHORTLINK_COGNITO_USER_POOL_ID")),
		ClientID:   strings.TrimSpace(getenv("SHORTLINK_COGNITO_CLIENT_ID")),
		Region:     strings.TrimSpace(getenv("SHORTLINK_COGNITO_REGION")),
		Username:   strings.TrimSpace(getenv("SHORTLINK_USERNAME")),
		Password:   getenv("SHORTLINK_PASSWORD"),
	}
	var missing []string
	for _, v := range []struct{ name, val string }{
		{"SHORTLINK_API_BASE", c.APIBase},
		{"SHORTLINK_COGNITO_USER_POOL_ID", c.UserPoolID},
		{"SHORTLINK_COGNITO_CLIENT_ID", c.ClientID},
		{"SHORTLINK_USERNAME", c.Username},
		{"SHORTLINK_PASSWORD", c.Password},
	} {
		if v.val == "" {
			missing = append(missing, v.name)
		}
	}
	if len(missing) > 0 {
		return config{}, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}

	region, _, ok := strings.Cut(c.UserPoolID, "_")
	if !ok || region == "" {
		return config{}, fmt.Errorf("SHORTLINK_COGNITO_USER_POOL_ID must look like <region>_<id>")
	}
	if c.Region == "" {
		c.Region = region
	} else if c.Region != region {
		return config{}, fmt.Errorf("SHORTLINK_COGNITO_REGION %q does not match the user pool's region %q", c.Region, region)
	}

	u, err := url.Parse(c.APIBase)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return config{}, fmt.Errorf("SHORTLINK_API_BASE must be an absolute URL such as https://go.example.com")
	}
	return c, nil
}
