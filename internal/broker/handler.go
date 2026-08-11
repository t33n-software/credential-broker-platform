// Package broker exposes the narrowly scoped credential-broker HTTP contract.
package broker

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/CyberT33N/credential-broker-platform/internal/config"
	"github.com/CyberT33N/credential-broker-platform/internal/githubapp"
)

const (
	tokenPath  = "/v1/github/installations/token"
	healthPath = "/healthz"
)

var randomReader io.Reader = rand.Reader

// Handler enforces the broker's repository and response contract.
type Handler struct {
	config    config.Config
	issuer    githubapp.Issuer
	logger    *slog.Logger
	now       func() time.Time
	requestID func() string
}

type tokenRequest struct {
	Host       string `json:"host"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
}

type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// NewHandler creates a broker endpoint backed by a repository-bound issuer.
func NewHandler(configuration config.Config, issuer githubapp.Issuer, logger *slog.Logger) (*Handler, error) {
	if issuer == nil {
		return nil, errors.New("GitHub installation-token issuer is required")
	}
	if len(configuration.AllowedRepositories) == 0 {
		return nil, errors.New("at least one allowed repository is required")
	}
	if configuration.MaxRequestBytes <= 0 {
		return nil, errors.New("maximum request bytes must be positive")
	}
	if configuration.MinimumTokenLifetime <= 0 {
		return nil, errors.New("minimum token lifetime must be positive")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Handler{
		config:    configuration,
		issuer:    issuer,
		logger:    logger,
		now:       time.Now,
		requestID: newRequestID,
	}, nil
}

// ServeHTTP handles health checks and repository-bound installation-token requests.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	requestID := handler.requestID()
	defer handler.recoverPanic(writer, requestID)

	switch request.URL.Path {
	case healthPath:
		handler.handleHealth(writer, request)
	case tokenPath:
		handler.handleToken(writer, request, requestID)
	default:
		writeJSON(writer, http.StatusNotFound, errorResponse{Error: "not found"})
	}
}

func (handler *Handler) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (handler *Handler) handleToken(writer http.ResponseWriter, request *http.Request, requestID string) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}

	requestedRepository, err := handler.decodeRequest(writer, request)
	if err != nil {
		handler.logger.Warn("rejected invalid token request", "request_id", requestID, "error", err)
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "invalid request"})
		return
	}
	if !handler.config.RepositoryAllowed(requestedRepository) {
		handler.logger.Warn(
			"rejected unauthorized repository",
			"request_id", requestID,
			"host", requestedRepository.Host,
			"owner", requestedRepository.Owner,
			"repository", requestedRepository.Name,
		)
		writeJSON(writer, http.StatusForbidden, errorResponse{Error: "repository is not authorized"})
		return
	}

	issued, err := handler.issuer.Mint(request.Context(), requestedRepository.Name)
	if err != nil {
		handler.logger.Error(
			"failed to mint installation token",
			"request_id", requestID,
			"host", requestedRepository.Host,
			"owner", requestedRepository.Owner,
			"repository", requestedRepository.Name,
		)
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: "credential issuance failed"})
		return
	}
	if !issued.ExpiresAt.After(handler.now().Add(handler.config.MinimumTokenLifetime)) {
		handler.logger.Error(
			"rejected too-short installation token",
			"request_id", requestID,
			"host", requestedRepository.Host,
			"owner", requestedRepository.Owner,
			"repository", requestedRepository.Name,
		)
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: "credential issuance failed"})
		return
	}

	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writeJSON(writer, http.StatusOK, tokenResponse{
		AccessToken: issued.Value,
		ExpiresAt:   issued.ExpiresAt.UTC(),
	})
}

func (handler *Handler) decodeRequest(writer http.ResponseWriter, request *http.Request) (config.Repository, error) {
	if request.Body == nil {
		return config.Repository{}, errors.New("request body is required")
	}
	defer request.Body.Close()

	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, handler.config.MaxRequestBytes))
	decoder.DisallowUnknownFields()
	var payload tokenRequest
	if err := decoder.Decode(&payload); err != nil {
		return config.Repository{}, fmt.Errorf("decode JSON: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return config.Repository{}, err
	}

	repository := config.Repository{
		Host:  strings.ToLower(strings.TrimSpace(payload.Host)),
		Owner: strings.TrimSpace(payload.Owner),
		Name:  strings.TrimSpace(payload.Repository),
	}
	if repository.Host == "" || repository.Owner == "" || repository.Name == "" {
		return config.Repository{}, errors.New("host, owner, and repository are required")
	}
	if strings.ContainsAny(repository.Host, " \t\r\n") || strings.ContainsAny(repository.Owner, " \t\r\n") || strings.ContainsAny(repository.Name, " \t\r\n") {
		return config.Repository{}, errors.New("repository values must not contain whitespace")
	}
	return repository, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("request must contain exactly one JSON value")
	}
	return fmt.Errorf("decode trailing JSON: %w", err)
}

func (handler *Handler) recoverPanic(writer http.ResponseWriter, requestID string) {
	if recovered := recover(); recovered != nil {
		handler.logger.Error("recovered broker panic", "request_id", requestID)
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func newRequestID() string {
	var bytes [12]byte
	if _, err := io.ReadFull(randomReader, bytes[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(bytes[:])
}

var _ http.Handler = (*Handler)(nil)
