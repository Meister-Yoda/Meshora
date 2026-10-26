// Package assets provides embedded static assets for Headscale.
// All static files (favicon, CSS, SVG) are embedded here for
// centralized asset management.
package assets

import (
	_ "embed"
)

// Favicon is the embedded favicon.png file served at /favicon.ico
//
//go:embed favicon.png
var Favicon []byte

// CSS is the embedded style.css stylesheet used in HTML templates.
// Contains Material for MkDocs design system styles.
//
//go:embed style.css
var CSS string

// AdminCSS is the stylesheet of the web admin dashboard, served as a file
// under /admin/assets/ so the dashboard CSP can forbid inline styles.
//
//go:embed admin.css
var AdminCSS []byte

// SVG is the embedded headscale.svg logo used in HTML templates.
//
//go:embed headscale.svg
var SVG string

// Logo is the Meshora wordmark for light backgrounds.
//
//go:embed meshora-logo.png
var Logo []byte

// LogoDark is the Meshora wordmark for dark backgrounds.
//
//go:embed meshora-logo-dark.png
var LogoDark []byte

// EnrollmentJS progressively enhances the public connection guide with clipboard support.
//
//go:embed enrollment.js
var EnrollmentJS []byte

// ZonesJS enhances the native zone editor dialog with focus and dismissal handling.
//
//go:embed zones.js
var ZonesJS []byte
