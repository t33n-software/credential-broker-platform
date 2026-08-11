// Command broker starts the GitHub App installation-token broker.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/CyberT33N/credential-broker-platform/internal/app"
)

var run = app.Run

func main() {
	runtimeContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(runtimeContext, slog.New(slog.NewJSONHandler(os.Stdout, nil))); err != nil {
		slog.Error("broker terminated", "error", err)
	}
}
