package telemetry

import (
	_ "embed"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed embeds/sms_tool
var embeddedSmsTool []byte

// EnsureSMSToolBinary ensures that the sms_tool binary is present on the filesystem.
// If it does not exist, it writes the embedded binary once to /usr/bin/sms_tool or /usrdata/bin/sms_tool.
func EnsureSMSToolBinary() string {
	// Only extract embedded ARM binary on ARM-based modem target architectures
	if runtime.GOARCH != "arm" && runtime.GOARCH != "arm64" {
		return ""
	}

	targetPaths := []string{
		"/usr/bin/sms_tool",
		"/usrdata/bin/sms_tool",
		"/usr/local/bin/sms_tool",
	}

	for _, p := range targetPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	if len(embeddedSmsTool) == 0 {
		return ""
	}

	for _, target := range targetPaths {
		dir := filepath.Dir(target)
		_ = os.MkdirAll(dir, 0755)
		if err := os.WriteFile(target, embeddedSmsTool, 0755); err == nil {
			return target
		}
	}

	return ""
}
