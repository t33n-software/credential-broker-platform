package config

import (
	"strings"
	"testing"
	"time"

	"github.com/t33n-software/credential-broker-platform/internal/githubapp"
)

func TestLoadUsesConfiguredAndDefaultValues(t *testing.T) {
	t.Setenv(EnvAllowedRepositories, "github.com/acme/platform-client,github.example/acme/release")
	t.Setenv(EnvBrokerAppID, "42")
	t.Setenv(EnvBrokerInstallationID, "99")
	t.Setenv(EnvCredentialProfile, string(githubapp.CredentialProfileReleaseAutomation))
	t.Setenv(EnvBrokerPrivateKeyPath, "/var/run/key.pem")
	t.Setenv(EnvPort, "")
	t.Setenv(EnvBrokerAPIBaseURL, "")
	t.Setenv(EnvRequestTimeout, "")
	t.Setenv(EnvMaxRequestBytes, "")
	t.Setenv(EnvMinimumTokenLifetime, "")

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.Port != defaultPort {
		t.Fatalf("Port = %q, want %q", configuration.Port, defaultPort)
	}
	if !configuration.RepositoryAllowed(Repository{Host: "GITHUB.COM", Owner: "acme", Name: "platform-client"}) {
		t.Fatal("RepositoryAllowed() = false, want true")
	}
	if configuration.GitHubAPIBaseURL != defaultGitHubAPIBaseURL {
		t.Fatalf("GitHubAPIBaseURL = %q, want %q", configuration.GitHubAPIBaseURL, defaultGitHubAPIBaseURL)
	}
	if configuration.CredentialProfile != githubapp.CredentialProfileReleaseAutomation {
		t.Fatalf("CredentialProfile = %q, want %q", configuration.CredentialProfile, githubapp.CredentialProfileReleaseAutomation)
	}
	if configuration.RequestTimeout != defaultRequestTimeout {
		t.Fatalf("RequestTimeout = %s, want %s", configuration.RequestTimeout, defaultRequestTimeout)
	}
	if configuration.MaxRequestBytes != defaultMaxRequestBytes {
		t.Fatalf("MaxRequestBytes = %d, want %d", configuration.MaxRequestBytes, defaultMaxRequestBytes)
	}
	if configuration.MinimumTokenLifetime != defaultMinimumTokenLifetime {
		t.Fatalf("MinimumTokenLifetime = %s, want %s", configuration.MinimumTokenLifetime, defaultMinimumTokenLifetime)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	valid := map[string]string{
		EnvAllowedRepositories:  "github.com/acme/platform-client",
		EnvBrokerAppID:          "1",
		EnvBrokerInstallationID: "2",
		EnvCredentialProfile:    string(githubapp.CredentialProfileHotfixPropagationPublisher),
		EnvBrokerPrivateKeyPath: "/key.pem",
	}

	for name, value := range map[string]string{
		EnvPort:                 "70000",
		EnvAllowedRepositories:  "invalid",
		EnvBrokerAppID:          "0",
		EnvBrokerInstallationID: "-1",
		EnvCredentialProfile:    "untrusted",
		EnvBrokerPrivateKeyPath: " ",
		EnvBrokerAPIBaseURL:     "http://api.github.com",
		EnvRequestTimeout:       "-1s",
		EnvMaxRequestBytes:      "0",
		EnvMinimumTokenLifetime: "nonsense",
	} {
		t.Run(name, func(t *testing.T) {
			environment := cloneEnvironment(valid)
			environment[name] = value
			_, err := load(func(key string) string { return environment[key] })
			if err == nil {
				t.Fatalf("load() error = nil for %s=%q", name, value)
			}
		})
	}

	for _, name := range []string{
		EnvAllowedRepositories,
		EnvBrokerAppID,
		EnvBrokerInstallationID,
		EnvCredentialProfile,
		EnvBrokerPrivateKeyPath,
	} {
		t.Run("missing "+name, func(t *testing.T) {
			environment := cloneEnvironment(valid)
			delete(environment, name)
			_, err := load(func(key string) string { return environment[key] })
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("load() error = %v, want error mentioning %s", err, name)
			}
		})
	}

	configuration, err := load(func(key string) string { return valid[key] })
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if configuration.CredentialProfile != githubapp.CredentialProfileHotfixPropagationPublisher {
		t.Fatalf("CredentialProfile = %q, want %q", configuration.CredentialProfile, githubapp.CredentialProfileHotfixPropagationPublisher)
	}
}

func TestLoadAcceptsEveryFixedCredentialProfile(t *testing.T) {
	base := map[string]string{
		EnvAllowedRepositories:  "github.com/acme/platform-client",
		EnvBrokerAppID:          "1",
		EnvBrokerInstallationID: "2",
		EnvBrokerPrivateKeyPath: "/key.pem",
	}
	for _, profile := range []githubapp.CredentialProfile{
		githubapp.CredentialProfileReleaseAutomation,
		githubapp.CredentialProfileReconciliationPublisher,
		githubapp.CredentialProfileHotfixPropagationPublisher,
		githubapp.CredentialProfileReleaseCredentialVerification,
		githubapp.CredentialProfileHotfixDelivery,
	} {
		t.Run(string(profile), func(t *testing.T) {
			environment := cloneEnvironment(base)
			environment[EnvCredentialProfile] = string(profile)
			configuration, err := load(func(key string) string { return environment[key] })
			if err != nil {
				t.Fatalf("load() error = %v", err)
			}
			if configuration.CredentialProfile != profile {
				t.Fatalf("CredentialProfile = %q, want %q", configuration.CredentialProfile, profile)
			}
		})
	}
}

func TestPortValue(t *testing.T) {
	for _, testCase := range []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "default", raw: "", want: defaultPort, ok: true},
		{name: "valid", raw: " 443 ", want: "443", ok: true},
		{name: "zero", raw: "0"},
		{name: "too high", raw: "65536"},
		{name: "non numeric", raw: "abc"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := portValue(testCase.raw)
			if (err == nil) != testCase.ok {
				t.Fatalf("portValue(%q) error = %v, want success %t", testCase.raw, err, testCase.ok)
			}
			if got != testCase.want {
				t.Fatalf("portValue(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}

func TestRepositoryParsing(t *testing.T) {
	repository, err := parseRepository(" GitHub.COM /acme/platform-client ")
	if err != nil {
		t.Fatalf("parseRepository() error = %v", err)
	}
	if repository != (Repository{Host: "github.com", Owner: "acme", Name: "platform-client"}) {
		t.Fatalf("parseRepository() = %#v", repository)
	}

	for _, raw := range []string{
		"",
		"one/two",
		"one/two/three/four",
		"github.com//repo",
		"github.com/owner/",
		"github .com/owner/repo",
		"github.com/owner name/repo",
		"github.com/owner/repo name",
	} {
		if _, err := parseRepository(raw); err == nil {
			t.Errorf("parseRepository(%q) error = nil", raw)
		}
	}

	repositories, err := repositoriesValue("github.com/acme/platform-client,github.com/acme/platform-client")
	if err != nil {
		t.Fatalf("repositoriesValue() error = %v", err)
	}
	if len(repositories) != 1 {
		t.Fatalf("len(repositories) = %d, want 1", len(repositories))
	}
	if _, err := repositoriesValue(" "); err == nil {
		t.Fatal("repositoriesValue(empty) error = nil")
	}
}

func TestValueHelpers(t *testing.T) {
	if got, err := positiveIntegerValue("TEST", " 42 "); err != nil || got != "42" {
		t.Fatalf("positiveIntegerValue() = %q, %v", got, err)
	}
	for _, raw := range []string{"", "0", "-1", "x"} {
		if _, err := positiveIntegerValue("TEST", raw); err == nil {
			t.Errorf("positiveIntegerValue(%q) error = nil", raw)
		}
	}

	if got, err := httpsURLValue("URL", "https://api.example.test/base/", ""); err != nil || got != "https://api.example.test/base" {
		t.Fatalf("httpsURLValue() = %q, %v", got, err)
	}
	if got, err := httpsURLValue("URL", "", "https://fallback.example"); err != nil || got != "https://fallback.example" {
		t.Fatalf("httpsURLValue(fallback) = %q, %v", got, err)
	}
	for _, raw := range []string{
		"http://example.test",
		"https://user@example.test",
		"https://example.test?query=yes",
		"https://example.test#fragment",
		"::invalid::",
	} {
		if _, err := httpsURLValue("URL", raw, ""); err == nil {
			t.Errorf("httpsURLValue(%q) error = nil", raw)
		}
	}

	if got, err := durationValue("DURATION", "3s", time.Second); err != nil || got != 3*time.Second {
		t.Fatalf("durationValue() = %s, %v", got, err)
	}
	if got, err := durationValue("DURATION", "", time.Second); err != nil || got != time.Second {
		t.Fatalf("durationValue(fallback) = %s, %v", got, err)
	}
	for _, raw := range []string{"0s", "-1s", "bad"} {
		if _, err := durationValue("DURATION", raw, time.Second); err == nil {
			t.Errorf("durationValue(%q) error = nil", raw)
		}
	}

	if got, err := positiveInt64Value("SIZE", "2", 1); err != nil || got != 2 {
		t.Fatalf("positiveInt64Value() = %d, %v", got, err)
	}
	if got, err := positiveInt64Value("SIZE", "", 1); err != nil || got != 1 {
		t.Fatalf("positiveInt64Value(fallback) = %d, %v", got, err)
	}
	for _, raw := range []string{"0", "-1", "bad"} {
		if _, err := positiveInt64Value("SIZE", raw, 1); err == nil {
			t.Errorf("positiveInt64Value(%q) error = nil", raw)
		}
	}
}

func TestConfigRepositoryAllowedAndListenAddress(t *testing.T) {
	configuration := Config{
		Port: "8080",
		AllowedRepositories: map[Repository]struct{}{
			{Host: "github.com", Owner: "acme", Name: "platform-client"}: {},
		},
	}
	if !configuration.RepositoryAllowed(Repository{Host: "GITHUB.COM", Owner: "acme", Name: "platform-client"}) {
		t.Fatal("RepositoryAllowed() = false, want true")
	}
	if configuration.RepositoryAllowed(Repository{Host: "github.com", Owner: "acme", Name: "other"}) {
		t.Fatal("RepositoryAllowed() = true, want false")
	}
	if got := configuration.ListenAddress(); got != ":8080" {
		t.Fatalf("ListenAddress() = %q, want %q", got, ":8080")
	}
}

func FuzzParseRepository(f *testing.F) {
	for _, value := range []string{
		"github.com/acme/platform-client",
		"",
		"github.com/owner/repository/extra",
		"github .com/owner/repository",
	} {
		f.Add(value)
	}

	f.Fuzz(func(t *testing.T, value string) {
		_, _ = parseRepository(value)
	})
}

func cloneEnvironment(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
