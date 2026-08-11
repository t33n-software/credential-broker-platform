// Package config validates the broker's non-secret runtime configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CyberT33N/credential-broker-platform/internal/githubapp"
)

const (
	EnvPort                     = "PORT"
	EnvAllowedRepositories      = "BROKER_ALLOWED_REPOSITORIES"
	EnvBrokerAppID              = "BROKER_APP_ID"
	EnvBrokerInstallationID     = "BROKER_APP_INSTALLATION_ID"
	EnvCredentialProfile        = "BROKER_CREDENTIAL_PROFILE"
	EnvBrokerPrivateKeyPath     = "BROKER_PRIVATE_KEY_PATH"
	EnvBrokerAPIBaseURL         = "BROKER_API_BASE_URL"
	EnvRequestTimeout           = "BROKER_REQUEST_TIMEOUT"
	EnvMaxRequestBytes          = "BROKER_MAX_REQUEST_BYTES"
	EnvMinimumTokenLifetime     = "BROKER_MIN_TOKEN_LIFETIME"
	defaultPort                 = "8080"
	defaultGitHubAPIBaseURL     = "https://api.github.com"
	defaultRequestTimeout       = 10 * time.Second
	defaultMaxRequestBytes      = int64(4096)
	defaultMinimumTokenLifetime = 2 * time.Minute
)

// Repository is the one-repository credential boundary the broker enforces.
type Repository struct {
	Host  string
	Owner string
	Name  string
}

// Config contains the validated runtime contract of the broker.
type Config struct {
	Port                 string
	AllowedRepositories  map[Repository]struct{}
	GitHubAppID          string
	GitHubInstallationID string
	CredentialProfile    githubapp.CredentialProfile
	PrivateKeyPath       string
	GitHubAPIBaseURL     string
	RequestTimeout       time.Duration
	MaxRequestBytes      int64
	MinimumTokenLifetime time.Duration
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	port, err := portValue(getenv(EnvPort))
	if err != nil {
		return Config{}, err
	}

	allowed, err := repositoriesValue(getenv(EnvAllowedRepositories))
	if err != nil {
		return Config{}, err
	}

	appID, err := positiveIntegerValue(EnvBrokerAppID, getenv(EnvBrokerAppID))
	if err != nil {
		return Config{}, err
	}
	installationID, err := positiveIntegerValue(EnvBrokerInstallationID, getenv(EnvBrokerInstallationID))
	if err != nil {
		return Config{}, err
	}

	credentialProfile, err := githubapp.ParseCredentialProfile(getenv(EnvCredentialProfile))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", EnvCredentialProfile, err)
	}

	privateKeyPath := strings.TrimSpace(getenv(EnvBrokerPrivateKeyPath))
	if privateKeyPath == "" {
		return Config{}, requiredValueError(EnvBrokerPrivateKeyPath)
	}

	apiBaseURL, err := httpsURLValue(EnvBrokerAPIBaseURL, getenv(EnvBrokerAPIBaseURL), defaultGitHubAPIBaseURL)
	if err != nil {
		return Config{}, err
	}
	requestTimeout, err := durationValue(EnvRequestTimeout, getenv(EnvRequestTimeout), defaultRequestTimeout)
	if err != nil {
		return Config{}, err
	}
	maxRequestBytes, err := positiveInt64Value(EnvMaxRequestBytes, getenv(EnvMaxRequestBytes), defaultMaxRequestBytes)
	if err != nil {
		return Config{}, err
	}
	minimumTokenLifetime, err := durationValue(EnvMinimumTokenLifetime, getenv(EnvMinimumTokenLifetime), defaultMinimumTokenLifetime)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:                 port,
		AllowedRepositories:  allowed,
		GitHubAppID:          appID,
		GitHubInstallationID: installationID,
		CredentialProfile:    credentialProfile,
		PrivateKeyPath:       privateKeyPath,
		GitHubAPIBaseURL:     apiBaseURL,
		RequestTimeout:       requestTimeout,
		MaxRequestBytes:      maxRequestBytes,
		MinimumTokenLifetime: minimumTokenLifetime,
	}, nil
}

func portValue(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultPort, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("%s must be a TCP port between 1 and 65535", EnvPort)
	}
	return strconv.Itoa(port), nil
}

func repositoriesValue(raw string) (map[Repository]struct{}, error) {
	values := strings.Split(strings.TrimSpace(raw), ",")
	if len(values) == 0 || (len(values) == 1 && values[0] == "") {
		return nil, requiredValueError(EnvAllowedRepositories)
	}

	repositories := make(map[Repository]struct{}, len(values))
	for _, rawRepository := range values {
		repository, err := parseRepository(rawRepository)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", EnvAllowedRepositories, err)
		}
		repositories[repository] = struct{}{}
	}
	return repositories, nil
}

func parseRepository(raw string) (Repository, error) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 3 {
		return Repository{}, errors.New("each repository must use host/owner/repository")
	}

	repository := Repository{
		Host:  strings.ToLower(strings.TrimSpace(parts[0])),
		Owner: strings.TrimSpace(parts[1]),
		Name:  strings.TrimSpace(parts[2]),
	}
	if repository.Host == "" || repository.Owner == "" || repository.Name == "" {
		return Repository{}, errors.New("repository values must not be empty")
	}
	if strings.ContainsAny(repository.Host, " \t\r\n") || strings.ContainsAny(repository.Owner, " \t\r\n") || strings.ContainsAny(repository.Name, " \t\r\n") {
		return Repository{}, errors.New("repository values must not contain whitespace")
	}
	return repository, nil
}

func positiveIntegerValue(name, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", requiredValueError(name)
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return "", fmt.Errorf("%s must be a positive integer", name)
	}
	return strconv.FormatUint(parsed, 10), nil
}

func httpsURLValue(name, raw, fallback string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s must be an HTTPS base URL without credentials, query, or fragment", name)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func durationValue(name, raw string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return duration, nil
}

func positiveInt64Value(name, raw string, fallback int64) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func requiredValueError(name string) error {
	return fmt.Errorf("%s must be configured", name)
}

// RepositoryAllowed reports whether the requested repository exactly matches
// one configured credential boundary.
func (config Config) RepositoryAllowed(repository Repository) bool {
	_, allowed := config.AllowedRepositories[Repository{
		Host:  strings.ToLower(strings.TrimSpace(repository.Host)),
		Owner: strings.TrimSpace(repository.Owner),
		Name:  strings.TrimSpace(repository.Name),
	}]
	return allowed
}

// ListenAddress returns the loopback-independent Cloud Run listener address.
func (config Config) ListenAddress() string {
	return net.JoinHostPort("", config.Port)
}
