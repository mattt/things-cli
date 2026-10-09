package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattt/things-cli/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cmd.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
