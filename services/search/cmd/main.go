package main

import (
	"context"

	"search/internal"
)

func main() {
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
