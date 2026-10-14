package main

import (
	"context"
	"log/slog"
	"os"

	"api-gateway/internal"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
