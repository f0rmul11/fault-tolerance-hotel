package main

import (
	"context"
	"log/slog"
	"os"

	"check-in/internal"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
