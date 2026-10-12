package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticAssetsFSDefaultUsesEmbed(t *testing.T) {
	fsys, err := staticAssetsFS()
	if err != nil {
		t.Fatalf("默认模式应返回 embed FS: %v", err)
	}
	body, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatalf("embed index.html 应可读: %v", err)
	}
	if !strings.Contains(string(body), "reader-parity.js") {
		t.Fatalf("embed index.html 应引用 reader-parity.js")
	}
}

func TestStaticAssetsFSOverrideServesDiskFiles(t *testing.T) {
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>dev-override</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ZAPP_STATIC_DIR", dir)
	fsys, err := staticAssetsFS()
	if err != nil {
		t.Fatalf("覆盖模式应返回磁盘 FS: %v", err)
	}
	body, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatalf("磁盘 index.html 应可读: %v", err)
	}
	if !strings.Contains(string(body), "dev-override") {
		t.Fatalf("应读到磁盘覆盖内容, 实际: %s", body)
	}
}

func TestValidateStaticDirOverrideRejectsMissingDir(t *testing.T) {
	t.Setenv("ZAPP_STATIC_DIR", filepath.Join(t.TempDir(), "nope"))
	if err := validateStaticDirOverride(); err == nil {
		t.Fatalf("目录缺少 static/index.html 时应报错")
	}
}

func TestValidateStaticDirOverrideAcceptsValidDir(t *testing.T) {
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZAPP_STATIC_DIR", dir)
	if err := validateStaticDirOverride(); err != nil {
		t.Fatalf("合法目录不应报错: %v", err)
	}
}
