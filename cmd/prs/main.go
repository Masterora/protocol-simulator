package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"gytest-go/internal/runner"
)

// Release builds set this from wails.json; no desktop packages are linked.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runner.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr, version))
}
