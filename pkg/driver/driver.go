// Package driver implements the COSI Identity and Provisioner gRPC services,
// mapping COSI calls onto the h3llo.cloud S3 management API.
package driver

import (
	"fmt"
	"os"
	"strings"

	"github.com/vmax/cosi-driver-h3/pkg/h3llo"
)

// DriverName is the COSI provisioner identifier. Must match the
// `driverName` field of any BucketClass / BucketAccessClass that targets
// this driver.
const DriverName = "s3.h3llo.cloud"

// Config holds runtime configuration, normally sourced from env vars.
type Config struct {
	// S3Endpoint is the S3 data-plane endpoint handed to bucket consumers
	// (NOT the management API). e.g. https://storage.h3llo.cloud
	S3Endpoint string
	// Region advertised to consumers.
	Region string
	// h3llo public API (HMAC-signed).
	APIBaseURL string
	KeyID      string
	SecretKey  string
	ProjectID  string
}

// ConfigFromEnv loads Config from environment variables and validates it.
func ConfigFromEnv() (Config, error) {
	c := Config{
		S3Endpoint: getenv("H3LLO_S3_ENDPOINT", "https://storage.h3llo.cloud"),
		Region:     getenv("H3LLO_REGION", "us-east-1"),
		APIBaseURL: getenv("H3_API_ENDPOINT", "https://api.h3llo.cloud"),
		KeyID:      os.Getenv("H3_KEY_ID"),
		SecretKey:  os.Getenv("H3_SECRET_KEY"),
		ProjectID:  os.Getenv("H3LLO_PROJECT_ID"),
	}
	var missing []string
	if c.KeyID == "" {
		missing = append(missing, "H3_KEY_ID")
	}
	if c.SecretKey == "" {
		missing = append(missing, "H3_SECRET_KEY")
	}
	if c.ProjectID == "" {
		missing = append(missing, "H3LLO_PROJECT_ID")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// newClient builds the h3llo API client from Config.
func (c Config) newClient() *h3llo.Client {
	return h3llo.New(h3llo.Config{
		BaseURL:   c.APIBaseURL,
		KeyID:     c.KeyID,
		SecretKey: c.SecretKey,
		ProjectID: c.ProjectID,
	})
}
