package main

import (
	"context"
	"fmt"
	"os"

	"webrtc-sip-gateway/internal/dbbootstrap"
)

func main() {
	result, err := dbbootstrap.Run(context.Background(), dbbootstrap.ConfigFromEnv())
	if err != nil {
		fmt.Fprintf(os.Stderr, "database bootstrap failed (state=%s): %v\n", result.State, err)
		os.Exit(1)
	}
}
