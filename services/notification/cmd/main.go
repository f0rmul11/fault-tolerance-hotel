package main

import (
	"context"

	"notification/internal"
)

func main() {
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
