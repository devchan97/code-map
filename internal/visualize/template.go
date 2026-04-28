// Package visualize renders a static graph.html for a codemap index.
package visualize

import _ "embed"

//go:embed graph.html.tmpl
var graphTemplate string
