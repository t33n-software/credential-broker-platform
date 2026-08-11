// Package app wires the broker's runtime dependencies.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/CyberT33N/credential-broker-platform/internal/broker"
	"github.com/CyberT33N/credential-broker-platform/internal/config"
	"github.com/CyberT33N/credential-broker-platform/internal/githubapp"
)

const shutdownTimeout = 10 * time.Second

var (
	newGitHubClient  = githubapp.NewClient
	newBrokerHandler = broker.NewHandler
)

type handlerBuilder func(*slog.Logger) (http.Handler, string, error)

type httpServer interface {
	Serve(net.Listener) error
	Shutdown(context.Context) error
}

type serverFactory func(string, http.Handler) httpServer

type listenerFactory func(string, string) (net.Listener, error)

// Run starts the broker and shuts it down gracefully when ctx is cancelled.
func Run(ctx context.Context, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return run(ctx, logger, buildHandler, net.Listen, newHTTPServer)
}

func run(ctx context.Context, logger *slog.Logger, build handlerBuilder, listen listenerFactory, newServer serverFactory) error {
	if ctx == nil {
		return errors.New("runtime context is required")
	}
	handler, address, err := build(logger)
	if err != nil {
		return err
	}
	listener, err := listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	server := newServer(address, handler)
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve broker: %w", err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("gracefully shut down broker: %w", err)
		}
		return nil
	}
}

func buildHandler(logger *slog.Logger) (http.Handler, string, error) {
	configuration, err := config.Load()
	if err != nil {
		return nil, "", fmt.Errorf("load broker configuration: %w", err)
	}
	privateKeyPEM, err := os.ReadFile(configuration.PrivateKeyPath)
	if err != nil {
		return nil, "", fmt.Errorf("read GitHub App private key: %w", err)
	}
	privateKey, err := githubapp.LoadPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, "", fmt.Errorf("parse GitHub App private key: %w", err)
	}
	issuer, err := newGitHubClient(
		configuration.GitHubAppID,
		configuration.GitHubInstallationID,
		configuration.CredentialProfile,
		privateKey,
		configuration.GitHubAPIBaseURL,
		&http.Client{Timeout: configuration.RequestTimeout},
		nil,
	)
	if err != nil {
		return nil, "", fmt.Errorf("configure GitHub App issuer: %w", err)
	}
	handler, err := newBrokerHandler(configuration, issuer, logger)
	if err != nil {
		return nil, "", fmt.Errorf("configure broker handler: %w", err)
	}
	return handler, configuration.ListenAddress(), nil
}

func newHTTPServer(address string, handler http.Handler) httpServer {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 * 1024,
	}
}
