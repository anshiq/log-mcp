package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

func Dist() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("webui: embedded dist missing: " + err.Error())
	}
	return sub
}
