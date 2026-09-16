// Package cmd wires forsight's CLI (cobra): "forsight run" starts the agent,
// "forsight demo" runs it against a bounded synthetic stream, "forsight
// version" prints the build version.
package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "forsight",
		Short:         "forsight is a self-contained observability agent",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRunCmd(), newDemoCmd(), newVersionCmd())
	return root
}

// Execute runs the CLI to completion.
func Execute(ctx context.Context) error {
	return newRootCmd().ExecuteContext(ctx)
}
