// Command codemap is the entrypoint for the codemap CLI.
package main

import (
	"os"

	"github.com/devchan97/code-map/internal/cli"
	"github.com/devchan97/code-map/internal/core"

	// Side-effect imports register all language parsers with the parser registry.
	// Each init() calls parser.Register so the pipeline can dispatch by language tag.
	_ "github.com/devchan97/code-map/internal/parser/cpp"
	_ "github.com/devchan97/code-map/internal/parser/csharp"
	_ "github.com/devchan97/code-map/internal/parser/golang"
	_ "github.com/devchan97/code-map/internal/parser/java"
	_ "github.com/devchan97/code-map/internal/parser/python"
	_ "github.com/devchan97/code-map/internal/parser/rust"
	_ "github.com/devchan97/code-map/internal/parser/ts"
)

func main() {
	if err := cli.NewRoot().Execute(); err != nil {
		os.Exit(core.ExitCode(err))
	}
}
