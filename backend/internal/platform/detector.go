package platform

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// Identity holds parsed hardware profile matching hw_profile.sh spec.
type Identity struct {
	Model      string `json:"model"`       // e.g. "RG501QEU_VD" or "RM520NGL_VC"
	Revision   string `json:"revision"`    // e.g. "RG501QEUAAR12A08M4G"
	SoC        string `json:"soc"`         // e.g. "SDX55" or "SDX6X"
	CustomName string `json:"custom_name"` // e.g. "STD"
	Serial     string `json:"serial"`      // From /proc/cmdline androidboot.serialno
	IsSDX55    bool   `json:"is_sdx55"`    // True on RG501Q-EU
	IsSDX65    bool   `json:"is_sdx65"`    // True on RM520N-GL
}

const (
	DefaultVersionFile = "/etc/quectel-project-version"
	DefaultCmdlineFile = "/proc/cmdline"
)

var (
	reField  = regexp.MustCompile(`^([A-Za-z]+)\s+([A-Za-z]+)\s*:\s*(.*)$`)
	reSerial = regexp.MustCompile(`androidboot\.serialno=([^\s]+)`)
)

// DetectIdentity parses Quectel project version and kernel cmdline.
func DetectIdentity(versionFile, cmdlineFile string) Identity {
	if versionFile == "" {
		versionFile = DefaultVersionFile
	}
	if cmdlineFile == "" {
		cmdlineFile = DefaultCmdlineFile
	}

	id := Identity{
		Model:      "unknown",
		Revision:   "unknown",
		SoC:        "unknown",
		CustomName: "unknown",
		Serial:     "unknown",
	}

	// 1. Parse /etc/quectel-project-version
	if file, err := os.Open(versionFile); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			matches := reField.FindStringSubmatch(line)
			if len(matches) == 4 {
				w1 := strings.ToLower(matches[1])
				w2 := strings.ToLower(matches[2])
				val := strings.TrimSpace(matches[3])

				if w1 == "project" && w2 == "name" {
					id.Model = val
				} else if w1 == "project" && w2 == "rev" {
					id.Revision = val
				} else if w1 == "branch" && w2 == "name" {
					id.SoC = val
				} else if w1 == "custom" && w2 == "name" {
					id.CustomName = val
				}
			}
		}
	}

	// 2. Parse /proc/cmdline for serial
	if data, err := os.ReadFile(cmdlineFile); err == nil {
		cmdline := string(data)
		if sm := reSerial.FindStringSubmatch(cmdline); len(sm) == 2 {
			id.Serial = sm[1]
		}
	}

	// 3. Flags
	id.IsSDX55 = strings.Contains(strings.ToUpper(id.SoC), "SDX55") || strings.Contains(strings.ToUpper(id.Model), "RG501Q")
	id.IsSDX65 = strings.Contains(strings.ToUpper(id.SoC), "SDX6") || strings.Contains(strings.ToUpper(id.Model), "RM520N")

	return id
}
