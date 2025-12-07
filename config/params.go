package config

import (
	"context"
	"fmt"
	"os"
	"time"

	parametermanager "cloud.google.com/go/parametermanager/apiv1"
	"cloud.google.com/go/parametermanager/apiv1/parametermanagerpb"
	"gopkg.in/yaml.v3"
)

// ParameterConfig represents YAML config from Parameter Manager
type ParameterConfig struct {
	Port                string `yaml:"port"`
	DBUser              string `yaml:"db_user"`
	DBName              string `yaml:"db_name"`
	CloudSQLEnabled     bool   `yaml:"cloudsql_enabled"`
	CloudSQLInstance    string `yaml:"cloudsql_instance"`
	HealthCheckCacheTTL int    `yaml:"health_check_cache_ttl"`
	VPSBearerToken      string `yaml:"vps_bearer_token"`
	VPSRequestTimeout   int    `yaml:"vps_request_timeout"`
}

// LoadFromParameterManager loads config from GCP Parameter Manager
func LoadFromParameterManager(ctx context.Context, project, paramName string) (*Config, error) {
	client, err := parametermanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create parameter manager client: %w", err)
	}
	defer client.Close()

	// Render parameter version to get resolved values (including secret references)
	name := fmt.Sprintf("projects/%s/locations/global/parameters/%s/versions/latest", project, paramName)
	req := &parametermanagerpb.RenderParameterVersionRequest{
		Name: name,
	}

	resp, err := client.RenderParameterVersion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to render parameter version: %w", err)
	}

	payload := resp.GetRenderedPayload()
	return parseYAMLConfig(payload)
}

// parseYAMLConfig parses YAML payload into Config
// Priority: Parameter Manager value > environment variable > default
func parseYAMLConfig(payload []byte) (*Config, error) {
	var pc ParameterConfig
	if err := yaml.Unmarshal(payload, &pc); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	cfg := &Config{
		Port:                firstNonEmpty(pc.Port, os.Getenv("PORT"), "8080"),
		DBHost:              firstNonEmpty(os.Getenv("DB_HOST"), "localhost"),
		DBPort:              firstNonEmpty(os.Getenv("DB_PORT"), "5432"),
		DBUser:              firstNonEmpty(pc.DBUser, os.Getenv("DB_USER")),
		DBPassword:          os.Getenv("DB_PASSWORD"),
		DBName:              firstNonEmpty(pc.DBName, os.Getenv("DB_NAME")),
		DBSSLMode:           firstNonEmpty(os.Getenv("DB_SSLMODE"), "disable"),
		CloudSQLEnabled:     pc.CloudSQLEnabled || getBoolEnv("CLOUDSQL_ENABLED", false),
		CloudSQLInstance:    firstNonEmpty(pc.CloudSQLInstance, os.Getenv("CLOUDSQL_INSTANCE")),
		HealthCheckCacheTTL: time.Duration(firstNonZero(pc.HealthCheckCacheTTL, 30)) * time.Second,
		VPSBearerToken:      firstNonEmpty(pc.VPSBearerToken, os.Getenv("VPS_BEARER_TOKEN")),
		VPSRequestTimeout:   time.Duration(firstNonZero(pc.VPSRequestTimeout, 55)) * time.Second,
	}

	return cfg, nil
}

// GetProjectID returns the GCP project ID
func GetProjectID() string {
	if project := os.Getenv("GCP_PROJECT"); project != "" {
		return project
	}
	if project := os.Getenv("GOOGLE_CLOUD_PROJECT"); project != "" {
		return project
	}
	return "cloudsql-sv"
}

// GetParameterName returns the parameter name from env
func GetParameterName() string {
	return getEnv("PARAM_NAME", "lb-scrape-config")
}

// UseParameterManager returns true if Parameter Manager should be used
func UseParameterManager() bool {
	return getBoolEnv("USE_PARAM_MANAGER", false)
}

// firstNonEmpty returns the first non-empty string from the arguments
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstNonZero returns the first non-zero int from the arguments
func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}
