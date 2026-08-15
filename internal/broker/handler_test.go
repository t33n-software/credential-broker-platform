package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/t33n-software/credential-broker-platform/internal/config"
	"github.com/t33n-software/credential-broker-platform/internal/githubapp"
)

type issuerFunc func(context.Context, string) (githubapp.Token, error)

func (function issuerFunc) Mint(ctx context.Context, repository string) (githubapp.Token, error) {
	return function(ctx, repository)
}

func TestNewHandlerValidatesDependencies(t *testing.T) {
	valid := testConfig()
	for _, testCase := range []struct {
		name          string
		configuration config.Config
		issuer        githubapp.Issuer
	}{
		{name: "missing issuer", configuration: valid},
		{name: "missing repositories", configuration: config.Config{MaxRequestBytes: 1, MinimumTokenLifetime: time.Second}, issuer: issuerFunc(func(context.Context, string) (githubapp.Token, error) { return githubapp.Token{}, nil })},
		{name: "invalid maximum request bytes", configuration: config.Config{AllowedRepositories: valid.AllowedRepositories, MinimumTokenLifetime: time.Second}, issuer: issuerFunc(func(context.Context, string) (githubapp.Token, error) { return githubapp.Token{}, nil })},
		{name: "invalid minimum lifetime", configuration: config.Config{AllowedRepositories: valid.AllowedRepositories, MaxRequestBytes: 1}, issuer: issuerFunc(func(context.Context, string) (githubapp.Token, error) { return githubapp.Token{}, nil })},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewHandler(testCase.configuration, testCase.issuer, nil); err == nil {
				t.Fatal("NewHandler() error = nil")
			}
		})
	}

	handler, err := NewHandler(valid, issuerFunc(func(context.Context, string) (githubapp.Token, error) {
		return githubapp.Token{}, nil
	}), nil)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if handler.logger == nil || handler.now == nil || handler.requestID == nil {
		t.Fatalf("NewHandler() = %#v", handler)
	}
}

func TestHealthAndRouting(t *testing.T) {
	handler := testHandler(t, issuerFunc(func(context.Context, string) (githubapp.Token, error) {
		return githubapp.Token{}, nil
	}))

	for _, testCase := range []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantAllow  string
	}{
		{name: "health", method: http.MethodGet, path: healthPath, wantStatus: http.StatusOK},
		{name: "health wrong method", method: http.MethodPost, path: healthPath, wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodGet},
		{name: "token wrong method", method: http.MethodGet, path: tokenPath, wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodPost},
		{name: "not found", method: http.MethodGet, path: "/unknown", wantStatus: http.StatusNotFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(testCase.method, testCase.path, nil))
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, testCase.wantStatus)
			}
			if response.Header().Get("Allow") != testCase.wantAllow {
				t.Fatalf("Allow = %q, want %q", response.Header().Get("Allow"), testCase.wantAllow)
			}
			if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatalf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
		})
	}
}

func TestTokenRequestValidation(t *testing.T) {
	handler := testHandler(t, issuerFunc(func(context.Context, string) (githubapp.Token, error) {
		return githubapp.Token{}, nil
	}))

	for _, body := range []string{
		"",
		"{",
		`{"host":"github.com","owner":"acme","repository":"platform-client","extra":true}`,
		`{"host":"","owner":"acme","repository":"platform-client"}`,
		`{"host":"github.com","owner":"ac me","repository":"platform-client"}`,
		`{"host":"github.com","owner":"acme","repository":"platform-client"} {}`,
		strings.Repeat("x", int(testConfig().MaxRequestBytes)+1),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}

	request := httptest.NewRequest(http.MethodPost, tokenPath, nil)
	request.Body = nil
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("nil body status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestTokenRequestRejectsUnauthorizedRepository(t *testing.T) {
	handler := testHandler(t, issuerFunc(func(context.Context, string) (githubapp.Token, error) {
		t.Fatal("issuer must not be called")
		return githubapp.Token{}, nil
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, tokenRequestHTTP(http.MethodPost, `{"host":"github.com","owner":"acme","repository":"other"}`))

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestTokenRequestHandlesIssuerFailureWithoutLeakingToken(t *testing.T) {
	var logs bytes.Buffer
	configuration := testConfig()
	handler, err := NewHandler(configuration, issuerFunc(func(context.Context, string) (githubapp.Token, error) {
		return githubapp.Token{}, errors.New("installation-token-secret-value")
	}), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	handler.requestID = func() string { return "request-1" }

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, tokenRequestHTTP(http.MethodPost, validTokenRequest()))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	if strings.Contains(logs.String(), "installation-token-secret-value") {
		t.Fatalf("logs leaked error text: %s", logs.String())
	}
}

func TestTokenRequestRejectsShortLivedCredential(t *testing.T) {
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	handler := testHandler(t, issuerFunc(func(context.Context, string) (githubapp.Token, error) {
		return githubapp.Token{Value: "short", ExpiresAt: now.Add(time.Minute)}, nil
	}))
	handler.now = func() time.Time { return now }

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, tokenRequestHTTP(http.MethodPost, validTokenRequest()))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
}

func TestTokenRequestReturnsNoStoreCredential(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 19, 2, 0, 0, 0, time.UTC)
	handler := testHandler(t, issuerFunc(func(ctx context.Context, repository string) (githubapp.Token, error) {
		if ctx == nil || repository != "platform-client" {
			t.Fatalf("Mint(%v, %q)", ctx, repository)
		}
		return githubapp.Token{Value: "token-value", ExpiresAt: expiresAt}, nil
	}))
	handler.now = func() time.Time { return time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC) }

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, tokenRequestHTTP(http.MethodPost, validTokenRequest()))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("cache headers = %#v", response.Header())
	}
	if !strings.Contains(response.Body.String(), `"access_token":"token-value"`) || !strings.Contains(response.Body.String(), `"expires_at":"2026-07-19T02:00:00Z"`) {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestTokenRequestRecoversIssuerPanic(t *testing.T) {
	handler := testHandler(t, issuerFunc(func(context.Context, string) (githubapp.Token, error) {
		panic("unexpected")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, tokenRequestHTTP(http.MethodPost, validTokenRequest()))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestHelpers(t *testing.T) {
	decoder := jsonDecoder(`{}`)
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		t.Fatalf("rejectTrailingJSON(EOF) error = %v", err)
	}
	extra := jsonDecoder(`{} {}`)
	if err := extra.Decode(&value); err != nil {
		t.Fatalf("Decode(extra) error = %v", err)
	}
	if err := rejectTrailingJSON(extra); err == nil {
		t.Fatal("rejectTrailingJSON(extra JSON) error = nil")
	}
	if err := rejectTrailingJSON(jsonDecoder(`{`)); err == nil {
		t.Fatal("rejectTrailingJSON(invalid JSON) error = nil")
	}
	if got := newRequestID(); len(got) != 24 {
		t.Fatalf("newRequestID() = %q", got)
	}
	originalReader := randomReader
	defer func() { randomReader = originalReader }()
	randomReader = failingReader{}
	if got := newRequestID(); got != "unavailable" {
		t.Fatalf("newRequestID() = %q, want unavailable", got)
	}
}

func FuzzTokenRequestBoundary(f *testing.F) {
	for _, body := range []string{
		validTokenRequest(),
		"",
		"{",
		`{"host":"github.com","owner":"acme","repository":"platform-client","extra":true}`,
		`{"host":"github.com","owner":"acme","repository":"platform-client"} {}`,
	} {
		f.Add(body)
	}

	handler, err := NewHandler(testConfig(), issuerFunc(func(context.Context, string) (githubapp.Token, error) {
		return githubapp.Token{Value: "token", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}), nil)
	if err != nil {
		f.Fatalf("NewHandler() error = %v", err)
	}
	handler.requestID = func() string { return "fuzz-request" }
	f.Fuzz(func(t *testing.T, body string) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, tokenRequestHTTP(http.MethodPost, body))
		if response.Code < http.StatusOK || response.Code > http.StatusInternalServerError {
			t.Fatalf("status = %d", response.Code)
		}
	})
}

func testConfig() config.Config {
	return config.Config{
		AllowedRepositories: map[config.Repository]struct{}{
			{Host: "github.com", Owner: "acme", Name: "platform-client"}: {},
		},
		MaxRequestBytes:      256,
		MinimumTokenLifetime: 2 * time.Minute,
	}
}

func testHandler(t *testing.T, issuer githubapp.Issuer) *Handler {
	t.Helper()
	handler, err := NewHandler(testConfig(), issuer, nil)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	handler.requestID = func() string { return "test-request" }
	return handler
}

func tokenRequestHTTP(method, body string) *http.Request {
	return httptest.NewRequest(method, tokenPath, strings.NewReader(body))
}

func validTokenRequest() string {
	return `{"host":"github.com","owner":"acme","repository":"platform-client"}`
}

func jsonDecoder(value string) *json.Decoder {
	return json.NewDecoder(strings.NewReader(value))
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("randomness unavailable")
}
