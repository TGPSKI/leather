// Package ui embeds the leather DevTools / overview web UI so that
// `leather serve` can serve it at /ui/* from the binary, with no need
// for the operator to know where the repository's ui/ directory lives.
//
// Files embedded:
//   - devtools.html, index.html
//   - js/**/*.{js,css} (entire js/ tree, including the devtools/ subdir
//     and any leather-*.js helpers)
//
// The exported FS is rooted at the ui/ directory: index.html is at the
// root, so http.FileServer(http.FS(Assets)) serves "/" as index.html and
// "/devtools.html" / "/js/..." as the rest.
package ui

import "embed"

//go:embed devtools.html index.html js
var assets embed.FS

// Assets is the embedded UI file system, rooted at this ui/ directory.
func Assets() embed.FS { return assets }
