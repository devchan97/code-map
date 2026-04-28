// Package skill installs and renders SKILL.md for coding agents.
//
// NOTE: SKILL.md.tmpl is duplicated here from skill/SKILL.md.tmpl at the
// repo root. The //go:embed directive forbids ".." path components, so the
// canonical source lives at skill/SKILL.md.tmpl and a copy is kept at
// internal/skill/SKILL.md.tmpl for embedding.
//
// TODO: consolidate the duplication once Go's embed supports "../" or the
// file is moved to a location reachable from this package without "..".
package skill

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

//go:embed SKILL.md.tmpl
var skillTemplate string

// templateData holds the values substituted into SKILL.md.tmpl.
type templateData struct {
	BinaryName string
	Version    string
}

// Render returns the SKILL.md body for the given binary name and version.
func Render(binaryName, version string) (string, error) {
	tmpl, err := template.New("skill").Parse(skillTemplate)
	if err != nil {
		return "", fmt.Errorf("skill: parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData{
		BinaryName: binaryName,
		Version:    version,
	}); err != nil {
		return "", fmt.Errorf("skill: execute template: %w", err)
	}

	return buf.String(), nil
}
