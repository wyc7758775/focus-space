//go:build dev

package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// staticAssetsFS 返回 /static/ 与 index.html 的文件源。生产路径是打进二进制的
// embed FS；本地开发时设置 ZAPP_STATIC_DIR 指向本仓库根目录，静态资源直接从
// 磁盘读取，改 JS/CSS 后刷新浏览器即可生效，无需重新编译。no-cache 头在两种
// 模式下都保留，避免磁盘模式与 embed 版本混用。
func staticAssetsFS() (fs.FS, error) {
	if dir := strings.TrimSpace(os.Getenv("ZAPP_STATIC_DIR")); dir != "" {
		return fs.Sub(os.DirFS(dir), "static")
	}
	return fs.Sub(appFS, "static")
}

func validateStaticDirOverride() error {
	dir := strings.TrimSpace(os.Getenv("ZAPP_STATIC_DIR"))
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "static", "index.html")); err != nil {
		return fmt.Errorf("ZAPP_STATIC_DIR 无效（应指向含 static/ 的仓库根目录）: %w", err)
	}
	log.Printf("开发模式: 静态资源从磁盘读取 %s", dir)
	return nil
}
