// Command broker starts the GitHub App installation-token broker.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/t33n-software/credential-broker-platform/internal/app"
)

var (
	run         = app.Run
	commandArgs = os.Args
	version     = "devel"
)

func main() {
	if len(commandArgs) == 2 && commandArgs[1] == "--version" {
		fmt.Printf("broker %s\n", version)
		return
	}

	runtimeContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(runtimeContext, slog.New(slog.NewJSONHandler(os.Stdout, nil))); err != nil {
		slog.Error("broker terminated", "error", err)
	}
}
