// Package adminui embeds the reproducibly built administration interface.
package adminui

import "embed"

//go:embed dist
var assets embed.FS

// File reads only embedded build files, never the process's local filesystem.
func File(name string) ([]byte, error) {
	return assets.ReadFile("dist/" + name)
}
