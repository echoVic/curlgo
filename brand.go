package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/brand/header-icon.svg
var brandIconSVG []byte

// Parse the embedded mark once; the native renderer keeps it sharp at any scale.
var brandIcon = ui.MustParseSVG(brandIconSVG)
