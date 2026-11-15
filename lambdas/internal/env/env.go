// Package env reads the Lambda configuration from environment variables.
package env

import (
	"fmt"
	"os"
)

// Require returns the value of the environment variable name and panics when
// it is unset, so a misconfigured function fails at init instead of per request.
func Require(name string) string {
	v, ok := os.LookupEnv(name)
	if !ok {
		panic(fmt.Sprintf("environment variable %s is not set", name))
	}
	return v
}
