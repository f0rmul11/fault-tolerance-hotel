package main

import (
	"context"

	"api-gateway/internal"
)

func main() {
	ctx := context.Background()
	internal.New(ctx).Run(ctx)
}
