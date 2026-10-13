//go:build !dev

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionStaticAssetsIgnoreDiskOverrideFlow(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "static"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "static", "index.html"), []byte("stale disk page"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZAPP_STATIC_DIR", dir)
	assets, err := staticAssetsFS()
	if err != nil {
		t.Fatal(err)
	}
	body, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "reader-parity.js") || strings.Contains(string(body), "stale disk page") {
		t.Fatal("production must serve embedded assets")
	}
}
