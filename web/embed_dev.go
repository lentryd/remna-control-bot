//go:build dev

package web

import (
	"io/fs"
	"os"
)

// Dist is empty under -tags dev: `task dev` serves web/dist from disk (WEB_DIR),
// so rebuilding the frontend neither breaks nor requires a Go build.
var Dist fs.FS = emptyFS{}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, os.ErrNotExist }
