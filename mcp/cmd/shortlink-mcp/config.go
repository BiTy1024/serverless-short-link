package main

import (
	"fmt"
	"net/url"
	"strings"
)

// Environment variables read by the server and by the login subcommand.
const (
	envAPIBase      = "SHORTLINK_API_BASE"
	envRegion       = "SHORTLINK_COGNITO_REGION"
	envClientID     = "SHORTLINK_COGNITO_CLIENT_ID"
	envRefreshToken = "SHORTLINK_REFRESH_TOKEN"
)

// serverConfig is what the MCP server needs:
//
//	SHORTLINK_API_BASE           base URL of the API, e.g. https://go.example.com
//	SHORTLINK_COGNITO_REGION     region of the Cognito user pool, e.g. eu-central-1
//	SHORTLINK_COGNITO_CLIENT_ID  ID of the MCP app client
//	SHORTLINK_REFRESH_TOKEN      refresh token printed by `shortlink-mcp login`
type serverConfig struct {
	APIBase      string
	Region       string
	ClientID     string
	RefreshToken string
}

func serverConfigFromEnv(getenv func(string) string) (serverConfig, error) {
	c := serverConfig{
		APIBase:      strings.TrimSpace(getenv(envAPIBase)),
		Region:       strings.TrimSpace(getenv(envRegion)),
		ClientID:     strings.TrimSpace(getenv(envClientID)),
		RefreshToken: strings.TrimSpace(getenv(envRefreshToken)),
	}
	if err := requireVars([][2]string{
		{envAPIBase, c.APIBase},
		{envRegion, c.Region},
		{envClientID, c.ClientID},
		{envRefreshToken, c.RefreshToken},
	}); err != nil {
		if c.RefreshToken == "" {
			err = fmt.Errorf("%w (get a refresh token with `shortlink-mcp login`)", err)
		}
		return serverConfig{}, err
	}
	u, err := url.Parse(c.APIBase)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return serverConfig{}, fmt.Errorf("%s must be an absolute URL such as https://go.example.com", envAPIBase)
	}
	return c, nil
}

// loginConfig is what `shortlink-mcp login` needs; flags override the
// environment.
type loginConfig struct {
	Region   string
	ClientID string
}

func (c loginConfig) validate() error {
	return requireVars([][2]string{{envRegion + " or -region", c.Region}, {envClientID + " or -client-id", c.ClientID}})
}

// requireVars reports every empty value by name; values are never printed.
func requireVars(vars [][2]string) error {
	var missing []string
	for _, v := range vars {
		if v[1] == "" {
			missing = append(missing, v[0])
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing configuration: %s", strings.Join(missing, ", "))
	}
	return nil
}
