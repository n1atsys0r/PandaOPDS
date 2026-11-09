package webui

import _ "embed"

// Page is the existing single-file administration UI, embedded in the Go
// binary so the service can run from any working directory.
//
//go:embed page.html
var Page []byte
