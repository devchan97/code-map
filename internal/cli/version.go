package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devchan97/code-map/internal/encoder"
	"github.com/devchan97/code-map/internal/store"
)

// Version is the binary version string. Set at release time via:
//
//	-ldflags "-X github.com/devchan97/code-map/internal/cli.Version=v1.0.0"
var Version = "0.0.0-dev"

// GitSHA is the full git commit SHA of the build. Injected at link time via:
//
//	-ldflags "-X github.com/devchan97/code-map/internal/cli.GitSHA=<sha>"
//
// Falls back to "unknown" when built outside the release pipeline (e.g. plain
// `go build` without the ldflags).
var GitSHA = "unknown"

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print codemap version, schema version, and active embedder",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine the active embedder by probing encoder.Default.
			// In default (non-encoder) builds this always returns ErrUnsupported,
			// so embedderName stays "lexical".
			// In encoder builds, Default() succeeds when the model file is present,
			// in which case we report the encoder's Name(); otherwise still "lexical".
			embedderName := "lexical"
			if enc, err := encoder.Default(); err == nil && enc != nil {
				embedderName = enc.Name()
			}

			// Short SHA for human display (first 12 chars); full SHA kept in GitSHA.
			shortSHA := GitSHA
			if len(shortSHA) > 12 {
				shortSHA = shortSHA[:12]
			}

			fmt.Printf("codemap %s (%s)\n", Version, shortSHA)
			fmt.Printf("schema_ver: %d\n", store.SchemaVer)
			fmt.Printf("embedder:   %s\n", embedderName)
			return nil
		},
	}
	return cmd
}
