package githubapp

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewClientValidatesDependencies(t *testing.T) {
	key := testPrivateKey(t)

	for _, testCase := range []struct {
		name           string
		appID          string
		installationID string
		profile        CredentialProfile
		key            *rsa.PrivateKey
		apiBaseURL     string
	}{
		{name: "missing app ID", installationID: "2", key: key, apiBaseURL: "https://api.github.com"},
		{name: "missing installation ID", appID: "1", key: key, apiBaseURL: "https://api.github.com"},
		{name: "invalid credential profile", appID: "1", installationID: "2", profile: "invalid", key: key, apiBaseURL: "https://api.github.com"},
		{name: "missing key", appID: "1", installationID: "2", profile: CredentialProfileReleaseAutomation, apiBaseURL: "https://api.github.com"},
		{name: "invalid API base URL", appID: "1", installationID: "2", profile: CredentialProfileReleaseAutomation, key: key, apiBaseURL: "://"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewClient(testCase.appID, testCase.installationID, testCase.profile, testCase.key, testCase.apiBaseURL, nil, nil); err == nil {
				t.Fatal("NewClient() error = nil")
			}
		})
	}

	client, err := NewClient(" 1 ", " 2 ", CredentialProfileReconciliationPublisher, key, "https://api.github.com", nil, nil)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.appID != "1" || client.installationID != "2" || client.profile != CredentialProfileReconciliationPublisher || client.httpClient == nil || client.now == nil {
		t.Fatalf("NewClient() = %#v", client)
	}
}

func TestParseCredentialProfile(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value string
		want  CredentialProfile
		ok    bool
	}{
		{name: "missing"},
		{name: "release automation", value: string(CredentialProfileReleaseAutomation), want: CredentialProfileReleaseAutomation, ok: true},
		{name: "reconciliation publisher", value: string(CredentialProfileReconciliationPublisher), want: CredentialProfileReconciliationPublisher, ok: true},
		{name: "hotfix propagation publisher", value: string(CredentialProfileHotfixPropagationPublisher), want: CredentialProfileHotfixPropagationPublisher, ok: true},
		{name: "release credential verification", value: string(CredentialProfileReleaseCredentialVerification), want: CredentialProfileReleaseCredentialVerification, ok: true},
		{name: "hotfix delivery", value: string(CredentialProfileHotfixDelivery), want: CredentialProfileHotfixDelivery, ok: true},
		{name: "invalid", value: "untrusted"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ParseCredentialProfile(testCase.value)
			if (err == nil) != testCase.ok {
				t.Fatalf("ParseCredentialProfile(%q) error = %v, want success %t", testCase.value, err, testCase.ok)
			}
			if got != testCase.want {
				t.Fatalf("ParseCredentialProfile(%q) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

func TestLoadPrivateKey(t *testing.T) {
	key := testPrivateKey(t)
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	loadedPKCS1, err := LoadPrivateKey(pkcs1)
	if err != nil || loadedPKCS1.N.Cmp(key.N) != 0 {
		t.Fatalf("LoadPrivateKey(PKCS1) = %#v, %v", loadedPKCS1, err)
	}

	pkcs8Contents, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	loadedPKCS8, err := LoadPrivateKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Contents}))
	if err != nil || loadedPKCS8.N.Cmp(key.N) != 0 {
		t.Fatalf("LoadPrivateKey(PKCS8) = %#v, %v", loadedPKCS8, err)
	}

	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	ecdsaContents, err := x509.MarshalPKCS8PrivateKey(ecdsaKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey(ECDSA) error = %v", err)
	}
	for _, contents := range [][]byte{
		nil,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("invalid")}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecdsaContents}),
	} {
		if _, err := LoadPrivateKey(contents); err == nil {
			t.Fatal("LoadPrivateKey() error = nil")
		}
	}
}

func TestMintRequestsRepositoryBoundInstallationToken(t *testing.T) {
	key := testPrivateKey(t)
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name        string
		profile     CredentialProfile
		permissions map[string]string
	}{
		{
			name:    "release automation",
			profile: CredentialProfileReleaseAutomation,
			permissions: map[string]string{
				"actions":       "write",
				"contents":      "read",
				"pull_requests": "write",
			},
		},
		{
			name:    "reconciliation publisher",
			profile: CredentialProfileReconciliationPublisher,
			permissions: map[string]string{
				"contents":      "write",
				"pull_requests": "write",
			},
		},
		{
			name:    "hotfix propagation publisher",
			profile: CredentialProfileHotfixPropagationPublisher,
			permissions: map[string]string{
				"contents":      "write",
				"pull_requests": "write",
			},
		},
		{
			name:    "release credential verification",
			profile: CredentialProfileReleaseCredentialVerification,
			permissions: map[string]string{
				"contents": "read",
			},
		},
		{
			name:    "hotfix delivery",
			profile: CredentialProfileHotfixDelivery,
			permissions: map[string]string{
				"actions":       "read",
				"contents":      "read",
				"pull_requests": "read",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != "/api/v3/app/installations/34/access_tokens" {
					t.Fatalf("request = %s %s", request.Method, request.URL.Path)
				}
				if request.Header.Get("Accept") != "application/vnd.github+json" {
					t.Fatalf("Accept = %q", request.Header.Get("Accept"))
				}
				if request.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("Content-Type = %q", request.Header.Get("Content-Type"))
				}
				if request.Header.Get("User-Agent") != "credential-broker-platform" {
					t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
				}
				verifyJWT(t, strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "), &key.PublicKey, now)

				var body tokenRequest
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				if len(body.Repositories) != 1 || body.Repositories[0] != "platform-client" {
					t.Fatalf("Repositories = %#v", body.Repositories)
				}
				if !mapsEqual(body.Permissions, testCase.permissions) {
					t.Fatalf("Permissions = %#v, want %#v", body.Permissions, testCase.permissions)
				}

				writer.WriteHeader(http.StatusCreated)
				_, _ = writer.Write([]byte(`{"token":"installation-token","expires_at":"2026-07-19T02:00:00Z"}`))
			}))
			defer server.Close()

			client, err := NewClient("12", "34", testCase.profile, key, server.URL+"/api/v3", server.Client(), func() time.Time { return now })
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			issued, err := client.Mint(context.Background(), " platform-client ")
			if err != nil {
				t.Fatalf("Mint() error = %v", err)
			}
			if issued.Value != "installation-token" || !issued.ExpiresAt.Equal(time.Date(2026, time.July, 19, 2, 0, 0, 0, time.UTC)) {
				t.Fatalf("Mint() = %#v", issued)
			}
			if got := client.installationTokenURL(); got != server.URL+"/api/v3/app/installations/34/access_tokens" {
				t.Fatalf("installationTokenURL() = %q", got)
			}
		})
	}
}

func TestMintRejectsInvalidInputsAndResponses(t *testing.T) {
	key := testPrivateKey(t)
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)

	client, err := NewClient("1", "2", CredentialProfileReleaseAutomation, key, "https://api.github.com", nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	for _, repository := range []string{"", "owner/repository"} {
		if _, err := client.Mint(context.Background(), repository); err == nil {
			t.Errorf("Mint(%q) error = nil", repository)
		}
	}
	if _, err := client.Mint(nil, "repository"); err == nil {
		t.Fatal("Mint(nil) error = nil")
	}
	client.profile = CredentialProfile("untrusted")
	if _, err := client.Mint(context.Background(), "repository"); err == nil {
		t.Fatal("Mint(untrusted profile) error = nil")
	}
	client.profile = CredentialProfileReleaseAutomation

	for _, response := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "status", status: http.StatusForbidden, body: `{"message":"forbidden"}`},
		{name: "invalid JSON", status: http.StatusCreated, body: `{`},
		{name: "missing token", status: http.StatusCreated, body: `{"expires_at":"2026-07-19T02:00:00Z"}`},
		{name: "expired token", status: http.StatusCreated, body: `{"token":"x","expires_at":"2026-07-19T00:00:00Z"}`},
	} {
		t.Run(response.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(response.status)
				_, _ = writer.Write([]byte(response.body))
			}))
			defer server.Close()

			testClient, err := NewClient("1", "2", CredentialProfileReleaseAutomation, key, server.URL, server.Client(), func() time.Time { return now })
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if _, err := testClient.Mint(context.Background(), "repository"); err == nil {
				t.Fatal("Mint() error = nil")
			}
		})
	}
}

func TestSignedJWTRejectsInvalidPrivateKey(t *testing.T) {
	client, err := NewClient("1", "2", CredentialProfileReleaseAutomation, &rsa.PrivateKey{}, "https://api.github.com", nil, time.Now)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.signedJWT(); err == nil {
		t.Fatal("signedJWT() error = nil")
	}
}

func TestMintCoversInfrastructureFailures(t *testing.T) {
	key := testPrivateKey(t)
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	client, err := NewClient("1", "2", CredentialProfileReleaseAutomation, key, "https://api.github.com", nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	originalRequest := newRequest
	defer func() { newRequest = originalRequest }()
	newRequest = func(context.Context, string, string, io.Reader) (*http.Request, error) {
		return nil, errors.New("request")
	}
	if _, err := client.Mint(context.Background(), "repository"); err == nil {
		t.Fatal("Mint(request construction) error = nil")
	}

	newRequest = originalRequest
	client.apiBaseURL, _ = url.Parse("https://api.github.com")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network")
	})}
	if _, err := client.Mint(context.Background(), "repository"); err == nil {
		t.Fatal("Mint(network) error = nil")
	}
}

func TestJWTAndRequestEncodingFailures(t *testing.T) {
	key := testPrivateKey(t)
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	client, err := NewClient("1", "2", CredentialProfileReleaseAutomation, key, "https://api.github.com", nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	originalMarshal := marshalJSON
	originalReader := randomReader
	originalSign := signRSA
	defer func() {
		marshalJSON = originalMarshal
		randomReader = originalReader
		signRSA = originalSign
	}()

	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("header") }
	if _, err := client.signedJWT(); err == nil {
		t.Fatal("signedJWT(header failure) error = nil")
	}

	calls := 0
	marshalJSON = func(value any) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("payload")
		}
		return json.Marshal(value)
	}
	if _, err := client.signedJWT(); err == nil {
		t.Fatal("signedJWT(payload failure) error = nil")
	}

	randomReader = errorReader{}
	marshalJSON = json.Marshal
	signRSA = func(io.Reader, *rsa.PrivateKey, crypto.Hash, []byte) ([]byte, error) {
		return nil, errors.New("signing")
	}
	if _, err := client.signedJWT(); err == nil {
		t.Fatal("signedJWT(signing failure) error = nil")
	}
	if _, err := client.Mint(context.Background(), "repository"); err == nil {
		t.Fatal("Mint(signing failure) error = nil")
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Fatal("HTTP request must not be sent when body encoding fails")
	}))
	defer server.Close()
	client, err = NewClient("1", "2", CredentialProfileReleaseAutomation, key, server.URL, server.Client(), func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	calls = 0
	marshalJSON = func(value any) ([]byte, error) {
		calls++
		if calls == 3 {
			return nil, errors.New("body")
		}
		return json.Marshal(value)
	}
	randomReader = rand.Reader
	signRSA = originalSign
	if _, err := client.Mint(context.Background(), "repository"); err == nil {
		t.Fatal("Mint(body encoding failure) error = nil")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func testPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return key
}

func verifyJWT(t *testing.T, value string, key *rsa.PublicKey, now time.Time) {
	t.Helper()
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT parts = %d", len(parts))
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || string(header) != `{"alg":"RS256","typ":"JWT"}` {
		t.Fatalf("JWT header = %q, %v", header, err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("Decode payload error = %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if claims["iss"] != "12" || claims["iat"] != float64(now.Add(-jwtClockSkew).Unix()) || claims["exp"] != float64(now.Add(jwtLifetime).Unix()) {
		t.Fatalf("claims = %#v", claims)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("Decode signature error = %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("VerifyPKCS1v15() error = %v", err)
	}
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}
	return true
}
