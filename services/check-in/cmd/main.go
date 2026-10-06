package main

import (
	"context"

	"check-in/internal"
)

func main() {
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
