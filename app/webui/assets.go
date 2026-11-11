package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist/index.html
var Page []byte

//go:embed dist
var embedded embed.FS

// Assets is the built Vite asset tree, rooted at dist for /webui-assets/ serving.
var Assets fs.FS

func init() {
	var err error
	Assets, err = fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
}
