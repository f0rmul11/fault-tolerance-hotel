package main

import (
	"context"

	"room/internal"
)

func main() {
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
