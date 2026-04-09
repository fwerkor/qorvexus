package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"qorvexus/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "qorvexus: %v\n", err)
		os.Exit(1)
	}
}
