package main

import (
	"context"
	"fmt"
	"os"

	"qorvexus/internal/cli"
)

func main() {
	if err := cli.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "qorvexus: %v\n", err)
		os.Exit(1)
	}
}
