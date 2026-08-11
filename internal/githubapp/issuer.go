// Package githubapp mints repository-bound GitHub App installation tokens.
package githubapp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	jwtLifetime        = 9 * time.Minute
	jwtClockSkew       = 30 * time.Second
	maxGitHubBodyBytes = 8 * 1024
)

// CredentialProfile fixes the maximum GitHub permissions requested by a broker
// deployment. It is never accepted from the token request.
type CredentialProfile string

const (
	CredentialProfileReleaseAutomation             CredentialProfile = "release-automation"
	CredentialProfileReconciliationPublisher       CredentialProfile = "reconciliation-publisher"
	CredentialProfileHotfixPropagationPublisher    CredentialProfile = "hotfix-propagation-publisher"
	CredentialProfileReleaseCredentialVerification CredentialProfile = "release-credential-verification"
	CredentialProfileHotfixDelivery                CredentialProfile = "hotfix-delivery"
)

var (
	marshalJSON            = json.Marshal
	randomReader io.Reader = rand.Reader
	signRSA                = rsa.SignPKCS1v15
	newRequest             = http.NewRequestWithContext
)

// Token is a repository-bound, short-lived GitHub installation credential.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// Issuer mints installation credentials for one explicitly requested repository.
type Issuer interface {
	Mint(context.Context, string) (Token, error)
}

// Client implements installation-token minting with a GitHub App private key.
type Client struct {
	appID          string
	installationID string
	profile        CredentialProfile
	privateKey     *rsa.PrivateKey
	apiBaseURL     *url.URL
	httpClient     *http.Client
	now            func() time.Time
}

type tokenRequest struct {
	Repositories []string          `json:"repositories"`
	Permissions  map[string]string `json:"permissions"`
}

type tokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewClient constructs an issuer for exactly one GitHub App installation.
func NewClient(appID, installationID string, profile CredentialProfile, privateKey *rsa.PrivateKey, apiBaseURL string, httpClient *http.Client, now func() time.Time) (*Client, error) {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(installationID) == "" {
		return nil, errors.New("GitHub App ID and installation ID must be configured")
	}
	parsedProfile, err := ParseCredentialProfile(string(profile))
	if err != nil {
		return nil, err
	}
	if privateKey == nil {
		return nil, errors.New("GitHub App private key must be configured")
	}
	parsedBaseURL, err := url.Parse(strings.TrimSpace(apiBaseURL))
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, errors.New("GitHub API base URL must be valid")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &Client{
		appID:          strings.TrimSpace(appID),
		installationID: strings.TrimSpace(installationID),
		profile:        parsedProfile,
		privateKey:     privateKey,
		apiBaseURL:     parsedBaseURL,
		httpClient:     httpClient,
		now:            now,
	}, nil
}

// LoadPrivateKey parses an RSA GitHub App private key from a PEM file.
func LoadPrivateKey(contents []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(contents)
	if block == nil {
		return nil, errors.New("GitHub App private key is not valid PEM")
	}
	if privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return privateKey, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("GitHub App private key is not a supported RSA key")
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("GitHub App private key must be RSA")
	}
	return privateKey, nil
}

// ParseCredentialProfile validates the fixed server-side GitHub permission profile.
func ParseCredentialProfile(value string) (CredentialProfile, error) {
	switch CredentialProfile(strings.TrimSpace(value)) {
	case CredentialProfileReleaseAutomation:
		return CredentialProfileReleaseAutomation, nil
	case CredentialProfileReconciliationPublisher:
		return CredentialProfileReconciliationPublisher, nil
	case CredentialProfileHotfixPropagationPublisher:
		return CredentialProfileHotfixPropagationPublisher, nil
	case CredentialProfileReleaseCredentialVerification:
		return CredentialProfileReleaseCredentialVerification, nil
	case CredentialProfileHotfixDelivery:
		return CredentialProfileHotfixDelivery, nil
	default:
		return "", fmt.Errorf("unsupported GitHub credential profile %q", value)
	}
}

// Mint creates an installation token restricted to the requested repository.
func (client *Client) Mint(ctx context.Context, repository string) (Token, error) {
	if ctx == nil {
		return Token{}, errors.New("request context is required")
	}
	repository = strings.TrimSpace(repository)
	if repository == "" || strings.Contains(repository, "/") {
		return Token{}, errors.New("repository name must be a single non-empty segment")
	}

	jwt, err := client.signedJWT()
	if err != nil {
		return Token{}, err
	}
	permissions, err := client.installationTokenPermissions()
	if err != nil {
		return Token{}, err
	}
	body, err := marshalJSON(tokenRequest{
		Repositories: []string{repository},
		Permissions:  permissions,
	})
	if err != nil {
		return Token{}, fmt.Errorf("encode installation-token request: %w", err)
	}

	request, err := newRequest(ctx, http.MethodPost, client.installationTokenURL(), bytes.NewReader(body))
	if err != nil {
		return Token{}, fmt.Errorf("create installation-token request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+jwt)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "credential-broker-platform")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return Token{}, fmt.Errorf("request GitHub installation token: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxGitHubBodyBytes))
		return Token{}, fmt.Errorf("GitHub installation-token request returned %s", response.Status)
	}

	var issued tokenResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxGitHubBodyBytes))
	if err := decoder.Decode(&issued); err != nil {
		return Token{}, fmt.Errorf("decode GitHub installation token: %w", err)
	}
	if strings.TrimSpace(issued.Token) == "" || !issued.ExpiresAt.After(client.now()) {
		return Token{}, errors.New("GitHub returned an invalid installation token")
	}
	return Token{Value: issued.Token, ExpiresAt: issued.ExpiresAt.UTC()}, nil
}

func (client *Client) installationTokenPermissions() (map[string]string, error) {
	switch client.profile {
	case CredentialProfileReleaseAutomation:
		return map[string]string{
			"actions":       "write",
			"contents":      "read",
			"pull_requests": "write",
		}, nil
	case CredentialProfileReconciliationPublisher, CredentialProfileHotfixPropagationPublisher:
		return map[string]string{
			"contents":      "write",
			"pull_requests": "write",
		}, nil
	case CredentialProfileReleaseCredentialVerification:
		return map[string]string{
			"contents": "read",
		}, nil
	case CredentialProfileHotfixDelivery:
		return map[string]string{
			"actions":       "read",
			"contents":      "read",
			"pull_requests": "read",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported GitHub credential profile %q", client.profile)
	}
}

func (client *Client) installationTokenURL() string {
	target := *client.apiBaseURL
	target.Path = path.Join(target.Path, "app", "installations", client.installationID, "access_tokens")
	return target.String()
}

func (client *Client) signedJWT() (string, error) {
	now := client.now().UTC()
	header, err := marshalJSON(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("encode GitHub App JWT header: %w", err)
	}
	payload, err := marshalJSON(map[string]any{
		"iat": now.Add(-jwtClockSkew).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		"iss": client.appID,
	})
	if err != nil {
		return "", fmt.Errorf("encode GitHub App JWT payload: %w", err)
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := encodedHeader + "." + encodedPayload
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := signRSA(randomReader, client.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign GitHub App JWT: %w", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

var _ Issuer = (*Client)(nil)
