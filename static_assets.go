//go:build !dev

package main

import "io/fs"

// Production assets always travel with the executable, including on rollback.
func staticAssetsFS() (fs.FS, error) { return fs.Sub(appFS, "static") }

func validateStaticDirOverride() error { return nil }
