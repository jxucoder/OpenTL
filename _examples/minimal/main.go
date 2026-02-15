// Minimal example: starts a TeleCoder server with all defaults.
// Requires GITHUB_TOKEN and at least one of ANTHROPIC_API_KEY or OPENAI_API_KEY.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	telecoder "github.com/jxucoder/TeleCoder"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	app, err := telecoder.NewBuilder().Build()
	if err != nil {
		log.Fatalf("Failed to build app: %v", err)
	}

	if err := app.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
