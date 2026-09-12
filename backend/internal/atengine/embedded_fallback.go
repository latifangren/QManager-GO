package atengine

import (
	_ "embed"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed embeds/atcli_smd11
var embeddedAtcli []byte

// EnsureAtcliBinary ensures that the atcli_smd11 binary is present on the filesystem.
// If it does not exist, it writes the embedded binary once.
func EnsureAtcliBinary() string {
	// Only extract embedded ARM binary on ARM-based modem target architectures
	if runtime.GOARCH != "arm" && runtime.GOARCH != "arm64" {
		return ""
	}

	targetPaths := []string{
		"/usr/bin/atcli_smd11",
		"/usrdata/bin/atcli_smd11",
		"/usr/local/bin/atcli_smd11",
	}

	for _, p := range targetPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	if len(embeddedAtcli) == 0 {
		return ""
	}

	// Attempt to restore to target directories
	for _, target := range targetPaths {
		dir := filepath.Dir(target)
		_ = os.MkdirAll(dir, 0755)
		if err := os.WriteFile(target, embeddedAtcli, 0755); err == nil {
			return target
		}
	}

	return ""
}
