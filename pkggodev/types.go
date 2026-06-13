package pkggodev

import "fmt"

// Package is the record emitted for a search result from pkg.go.dev.
type Package struct {
	Rank       int    `json:"rank"`
	ImportPath string `json:"import_path"`
	Synopsis   string `json:"synopsis"`
	Version    string `json:"version"`
	Published  string `json:"published"`
	URL        string `json:"url"`
}

// Version is the record emitted for one entry in a module's version list.
type Version struct {
	Rank    int    `json:"rank"`
	Version string `json:"version"`
	URL     string `json:"url"`
}

// LatestInfo is the record emitted by the latest command.
type LatestInfo struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Time    string `json:"time"`
	URL     string `json:"url"`
}

// HashInfo is the record emitted by the hash command.
type HashInfo struct {
	Module    string `json:"module"`
	Version   string `json:"version"`
	Hash      string `json:"hash"`
	GoModHash string `json:"go_mod_hash"`
	URL       string `json:"url"`
}

// pkgURL returns the pkg.go.dev URL for an import path.
func pkgURL(base, importPath string) string {
	return base + "/" + importPath
}

// moduleVersionURL returns the pkg.go.dev URL for a module at a version.
func moduleVersionURL(base, module, version string) string {
	return fmt.Sprintf("%s/%s@%s", base, module, version)
}
