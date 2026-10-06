package main

import (
	"context"

	"availability/internal"
)

func main() {
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
