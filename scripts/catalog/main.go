// Command catalog writes catalog.json and SHA256SUMS for a release. It reads
// every plugins/<id>/plugin.toml and the tarballs in -dist (named
// <id>-<version>-<goos>-<goarch>.tar.gz), and points each asset at the
// release's download URL. The format is documented in TideMail's
// docs/plugins/distribution.md.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type manifest struct {
	ID          string          `toml:"id"`
	Name        string          `toml:"name"`
	Description string          `toml:"description"`
	Version     string          `toml:"version"`
	API         int             `toml:"api"`
	Homepage    string          `toml:"homepage"`
	Permissions map[string]bool `toml:"permissions"`
}

type asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type entry struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Version     string           `json:"version"`
	API         int              `json:"api"`
	Homepage    string           `json:"homepage,omitempty"`
	Permissions map[string]bool  `json:"permissions"`
	Assets      map[string]asset `json:"assets"`
}

func main() {
	dist := flag.String("dist", "dist", "directory holding the tarballs; catalog.json and SHA256SUMS are written there")
	repo := flag.String("repo", "allisonhere/tidemail-plugins", "GitHub owner/name that hosts the release")
	tag := flag.String("tag", "", "release tag the assets are attached to")
	plugins := flag.String("plugins", "plugins", "directory of plugin sources")
	flag.Parse()
	if *tag == "" {
		fail("-tag is required")
	}

	manifests, err := filepath.Glob(filepath.Join(*plugins, "*", "plugin.toml"))
	if err != nil || len(manifests) == 0 {
		fail("no plugin manifests under %s", *plugins)
	}
	sort.Strings(manifests)

	var entries []entry
	var sums []string
	for _, path := range manifests {
		var m manifest
		if _, err := toml.DecodeFile(path, &m); err != nil {
			fail("%s: %v", path, err)
		}
		e := entry{
			ID: m.ID, Name: m.Name, Description: m.Description, Version: m.Version, API: m.API,
			Homepage:    m.Homepage,
			Permissions: map[string]bool{},
			Assets:      map[string]asset{},
		}
		for k, on := range m.Permissions {
			if on {
				e.Permissions[k] = true
			}
		}
		prefix := m.ID + "-" + m.Version + "-"
		files, _ := filepath.Glob(filepath.Join(*dist, prefix+"*.tar.gz"))
		sort.Strings(files)
		for _, f := range files {
			platform := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), prefix), ".tar.gz")
			data, err := os.ReadFile(f)
			if err != nil {
				fail("%v", err)
			}
			h := sha256.Sum256(data)
			sum := hex.EncodeToString(h[:])
			e.Assets[platform] = asset{
				URL:    fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", *repo, *tag, filepath.Base(f)),
				SHA256: sum,
			}
			sums = append(sums, sum+"  "+filepath.Base(f))
		}
		if len(e.Assets) == 0 {
			fail("no tarballs for %s %s in %s", m.ID, m.Version, *dist)
		}
		entries = append(entries, e)
	}

	out, err := json.MarshalIndent(map[string]any{"schema": 1, "plugins": entries}, "", "  ")
	if err != nil {
		fail("%v", err)
	}
	if err := os.WriteFile(filepath.Join(*dist, "catalog.json"), append(out, '\n'), 0o644); err != nil {
		fail("%v", err)
	}
	sort.Strings(sums)
	if err := os.WriteFile(filepath.Join(*dist, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0o644); err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "catalog: "+format+"\n", args...)
	os.Exit(1)
}
