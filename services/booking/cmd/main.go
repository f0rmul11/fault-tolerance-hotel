package main

import (
	"context"

	"booking/internal"
)

func main() {
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
