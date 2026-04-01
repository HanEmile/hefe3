// Package templates embeds all HTML templates for standalone binary distribution.
package templates

import "embed"

//go:embed *.html
var FS embed.FS
