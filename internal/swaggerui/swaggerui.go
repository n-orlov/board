// Package swaggerui embeds Swagger UI 5.34.1 (swagger-ui-dist, Apache-2.0, see
// LICENSE and NOTICE here) so the binary serves /api/docs with no file beside
// it and no request to another host.
package swaggerui

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed assets
var assets embed.FS

// Page is the HTML of /api/docs. It loads every script and style from
// /api/docs/assets/, so it names no external host.
const Page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Board API</title>
<link rel="stylesheet" href="/api/docs/assets/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="/api/docs/assets/swagger-ui-bundle.js"></script>
<script src="/api/docs/assets/init.js"></script>
</body>
</html>
`

// Asset returns the embedded asset called name, and its Content-Type. ok is
// false for a name that is not one of the embedded assets.
func Asset(name string) (body []byte, contentType string, ok bool) {
	if name != path.Base(name) {
		return nil, "", false
	}
	body, err := fs.ReadFile(assets, "assets/"+name)
	if err != nil {
		return nil, "", false
	}
	switch path.Ext(name) {
	case ".js":
		return body, "text/javascript", true
	case ".css":
		return body, "text/css", true
	}
	return nil, "", false
}
