package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

func addTimeoutFlag(cmd *cobra.Command) {
	cmd.Flags().Duration("timeout", source.DefaultTimeout, "time limit for each plugin describe")
}

func timeoutFlag(cmd *cobra.Command) (time.Duration, error) {
	d, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("--timeout must be positive, got %s", d)
	}
	return d, nil
}
