package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/CyberT33N/credential-broker-platform/internal/broker"
	"github.com/CyberT33N/credential-broker-platform/internal/config"
	"github.com/CyberT33N/credential-broker-platform/internal/githubapp"
)

type testServer struct {
	serve    func(net.Listener) error
	shutdown func(context.Context) error
}

func (server testServer) Serve(listener net.Listener) error {
	return server.serve(listener)
}

func (server testServer) Shutdown(ctx context.Context) error {
	return server.shutdown(ctx)
}

type testListener struct {
	closed bool
	mutex  sync.Mutex
}

func (listener *testListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (listener *testListener) Close() error {
	listener.mutex.Lock()
	defer listener.mutex.Unlock()
	listener.closed = true
	return nil
}
func (listener *testListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestRunValidatesAndHandlesServerOutcomes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := http.NotFoundHandler()
	build := func(*slog.Logger) (http.Handler, string, error) { return handler, ":0", nil }
	listener := &testListener{}
	listen := func(string, string) (net.Listener, error) { return listener, nil }

	if err := run(nil, logger, build, listen, func(string, http.Handler) httpServer {
		return testServer{}
	}); err == nil {
		t.Fatal("run(nil) error = nil")
	}

	if err := run(context.Background(), logger, func(*slog.Logger) (http.Handler, string, error) {
		return nil, "", errors.New("build")
	}, listen, func(string, http.Handler) httpServer {
		return testServer{}
	}); err == nil {
		t.Fatal("run(build failure) error = nil")
	}

	if err := run(context.Background(), logger, build, func(string, string) (net.Listener, error) {
		return nil, errors.New("listen")
	}, func(string, http.Handler) httpServer {
		return testServer{}
	}); err == nil {
		t.Fatal("run(listen failure) error = nil")
	}

	for _, testCase := range []struct {
		name    string
		serve   error
		wantErr bool
	}{
		{name: "server closed", serve: http.ErrServerClosed},
		{name: "server failure", serve: errors.New("serve"), wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := run(context.Background(), logger, build, listen, func(string, http.Handler) httpServer {
				return testServer{
					serve:    func(net.Listener) error { return testCase.serve },
					shutdown: func(context.Context) error { return nil },
				}
			})
			if (err != nil) != testCase.wantErr {
				t.Fatalf("run() error = %v, want error %t", err, testCase.wantErr)
			}
		})
	}

	for _, testCase := range []struct {
		name        string
		shutdownErr error
		wantErr     bool
	}{
		{name: "graceful shutdown"},
		{name: "shutdown failure", shutdownErr: errors.New("shutdown"), wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			shutdownCalled := make(chan struct{})
			err := run(ctx, logger, build, listen, func(string, http.Handler) httpServer {
				return testServer{
					serve: func(net.Listener) error {
						<-shutdownCalled
						return http.ErrServerClosed
					},
					shutdown: func(context.Context) error {
						close(shutdownCalled)
						return testCase.shutdownErr
					},
				}
			})
			if (err != nil) != testCase.wantErr {
				t.Fatalf("run() error = %v, want error %t", err, testCase.wantErr)
			}
		})
	}
}

func TestBuildHandler(t *testing.T) {
	t.Setenv(config.EnvAllowedRepositories, "github.com/acme/platform-client")
	t.Setenv(config.EnvBrokerAppID, "1")
	t.Setenv(config.EnvBrokerInstallationID, "2")
	t.Setenv(config.EnvCredentialProfile, string(githubapp.CredentialProfileReleaseAutomation))
	t.Setenv(config.EnvBrokerAPIBaseURL, "https://api.github.com")
	t.Setenv(config.EnvPort, "8080")
	t.Setenv(config.EnvRequestTimeout, "1s")
	t.Setenv(config.EnvMaxRequestBytes, "128")
	t.Setenv(config.EnvMinimumTokenLifetime, "1m")

	keyPath := writePrivateKey(t)
	t.Setenv(config.EnvBrokerPrivateKeyPath, keyPath)
	handler, address, err := buildHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("buildHandler() error = %v", err)
	}
	if handler == nil || address != ":8080" {
		t.Fatalf("buildHandler() = %T, %q", handler, address)
	}

	t.Setenv(config.EnvBrokerAppID, "")
	if _, _, err := buildHandler(nil); err == nil {
		t.Fatal("buildHandler(config error) = nil")
	}

	t.Setenv(config.EnvBrokerAppID, "1")
	t.Setenv(config.EnvBrokerPrivateKeyPath, "missing")
	if _, _, err := buildHandler(nil); err == nil {
		t.Fatal("buildHandler(read error) = nil")
	}

	invalidPath := writeFile(t, []byte("invalid"))
	t.Setenv(config.EnvBrokerPrivateKeyPath, invalidPath)
	if _, _, err := buildHandler(nil); err == nil {
		t.Fatal("buildHandler(parse error) = nil")
	}

	t.Setenv(config.EnvBrokerPrivateKeyPath, keyPath)
	originalFactory := newGitHubClient
	defer func() { newGitHubClient = originalFactory }()
	newGitHubClient = func(string, string, githubapp.CredentialProfile, *rsa.PrivateKey, string, *http.Client, func() time.Time) (*githubapp.Client, error) {
		return nil, errors.New("issuer")
	}
	if _, _, err := buildHandler(nil); err == nil {
		t.Fatal("buildHandler(issuer error) = nil")
	}

	newGitHubClient = originalFactory
	originalHandler := newBrokerHandler
	defer func() { newBrokerHandler = originalHandler }()
	newBrokerHandler = func(config.Config, githubapp.Issuer, *slog.Logger) (*broker.Handler, error) {
		return nil, errors.New("handler")
	}
	if _, _, err := buildHandler(nil); err == nil {
		t.Fatal("buildHandler(handler error) = nil")
	}
}

func TestBuildHandlerPassesCredentialProfile(t *testing.T) {
	for _, profile := range []githubapp.CredentialProfile{
		githubapp.CredentialProfileReconciliationPublisher,
		githubapp.CredentialProfileHotfixPropagationPublisher,
		githubapp.CredentialProfileReleaseCredentialVerification,
		githubapp.CredentialProfileHotfixDelivery,
	} {
		t.Run(string(profile), func(t *testing.T) {
			t.Setenv(config.EnvAllowedRepositories, "github.com/acme/platform-client")
			t.Setenv(config.EnvBrokerAppID, "1")
			t.Setenv(config.EnvBrokerInstallationID, "2")
			t.Setenv(config.EnvCredentialProfile, string(profile))
			t.Setenv(config.EnvBrokerAPIBaseURL, "https://api.github.com")
			t.Setenv(config.EnvRequestTimeout, "1s")
			t.Setenv(config.EnvMaxRequestBytes, "128")
			t.Setenv(config.EnvMinimumTokenLifetime, "1m")
			t.Setenv(config.EnvBrokerPrivateKeyPath, writePrivateKey(t))

			originalFactory := newGitHubClient
			t.Cleanup(func() { newGitHubClient = originalFactory })

			var got githubapp.CredentialProfile
			newGitHubClient = func(appID, installationID string, profile githubapp.CredentialProfile, privateKey *rsa.PrivateKey, apiBaseURL string, httpClient *http.Client, now func() time.Time) (*githubapp.Client, error) {
				got = profile
				return originalFactory(appID, installationID, profile, privateKey, apiBaseURL, httpClient, now)
			}

			if _, _, err := buildHandler(slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
				t.Fatalf("buildHandler() error = %v", err)
			}
			if got != profile {
				t.Fatalf("CredentialProfile = %q, want %q", got, profile)
			}
		})
	}
}

func TestNewHTTPServer(t *testing.T) {
	server, ok := newHTTPServer(":8080", http.NotFoundHandler()).(*http.Server)
	if !ok {
		t.Fatalf("newHTTPServer() type = %T", server)
	}
	if server.Addr != ":8080" || server.ReadHeaderTimeout != 5*time.Second || server.MaxHeaderBytes != 8*1024 {
		t.Fatalf("newHTTPServer() = %#v", server)
	}
}

func TestRunUsesDefaultLogger(t *testing.T) {
	t.Setenv(config.EnvAllowedRepositories, "github.com/acme/platform-client")
	t.Setenv(config.EnvBrokerAppID, "1")
	t.Setenv(config.EnvBrokerInstallationID, "2")
	t.Setenv(config.EnvCredentialProfile, string(githubapp.CredentialProfileReleaseAutomation))
	t.Setenv(config.EnvBrokerAPIBaseURL, "https://api.github.com")
	t.Setenv(config.EnvRequestTimeout, "1s")
	t.Setenv(config.EnvMaxRequestBytes, "128")
	t.Setenv(config.EnvMinimumTokenLifetime, "1m")
	t.Setenv(config.EnvBrokerPrivateKeyPath, writePrivateKey(t))

	port := freePort(t)
	t.Setenv(config.EnvPort, port)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func writePrivateKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return writeFile(t, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func writeFile(t *testing.T, contents []byte) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "private-key-*")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if _, err := file.Write(contents); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return file.Name()
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	address := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return fmt.Sprintf("%d", address)
}
