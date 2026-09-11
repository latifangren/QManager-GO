package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"qmanager/internal/atengine"
	"qmanager/internal/config"
	"qmanager/internal/telemetry"
)

var (
	imeiBackupPath        = "/etc/qmanager/imei_backup.json"
	imeiRebootPendingFlag = "/tmp/qm_imei_reboot_pending"
)

// BackupImeiConfig matches frontend type.
type BackupImeiConfig struct {
	Enabled bool   `json:"enabled"`
	Imei    string `json:"imei"`
}

// CellularImeiHandler handles IMEI settings and Luhn validation.
type CellularImeiHandler struct {
	engine         *atengine.Engine
	poller         *telemetry.Poller
	configMgr      *config.Manager
	imeiBackupPath string
}

// NewCellularImeiHandler creates a new CellularImeiHandler.
func NewCellularImeiHandler(engine *atengine.Engine, poller *telemetry.Poller, configMgr *config.Manager, optionalConfigDir ...string) *CellularImeiHandler {
	backupP := imeiBackupPath
	if len(optionalConfigDir) > 0 && optionalConfigDir[0] != "" {
		backupP = filepath.Join(optionalConfigDir[0], "imei_backup.json")
	}
	return &CellularImeiHandler{
		engine:         engine,
		poller:         poller,
		configMgr:      configMgr,
		imeiBackupPath: backupP,
	}
}

// SetStoragePath sets custom backup file path for testing.
func (h *CellularImeiHandler) SetStoragePath(path string) {
	h.imeiBackupPath = path
}

// GetIMEI handles GET /api/v1/cellular/imei and GET /cgi-bin/quecmanager/cellular/imei.sh
func (h *CellularImeiHandler) GetIMEI(w http.ResponseWriter, r *http.Request) {
	currentImei := ""

	// Read IMEI via AT+GSN
	res, err := h.engine.Exec("AT+GSN")
	if err == nil {
		lines := strings.Split(res.Raw, "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if len(l) >= 14 && len(l) <= 16 && isDigitsOnly(l) {
				currentImei = l
				break
			}
		}
	}

	backup := readImeiBackupConfig()

	resp := map[string]interface{}{
		"success":      true,
		"current_imei": currentImei,
		"backup":       backup,
	}

	JSON(w, http.StatusOK, resp)
}

// ImeiSavePayload matches POST requests from frontend.
type ImeiSavePayload struct {
	Action     string `json:"action"`
	Imei       string `json:"imei,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
	BackupImei string `json:"backup_imei,omitempty"`
}

// SaveIMEI handles POST /api/v1/cellular/imei and POST /cgi-bin/quecmanager/cellular/imei.sh
func (h *CellularImeiHandler) SaveIMEI(w http.ResponseWriter, r *http.Request) {
	var p ImeiSavePayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	switch p.Action {
	case "set_imei":
		h.handleSetIMEI(w, p.Imei)
	case "save_backup":
		h.handleSaveBackup(w, p)
	case "reboot":
		h.handleReboot(w)
	default:
		Error(w, http.StatusBadRequest, "Unknown action")
	}
}

func (h *CellularImeiHandler) handleSetIMEI(w http.ResponseWriter, newImei string) {
	newImei = strings.TrimSpace(newImei)
	if newImei == "" {
		Error(w, http.StatusBadRequest, "IMEI cannot be empty")
		return
	}

	if len(newImei) == 14 && isDigitsOnly(newImei) {
		if cd, ok := CalculateLuhnCheckDigit(newImei); ok {
			newImei = newImei + string('0'+byte(cd))
		}
	}

	if !ValidateLuhnIMEI(newImei) {
		Error(w, http.StatusBadRequest, "Invalid IMEI (fails 15-digit format or Luhn checksum)")
		return
	}

	// Write via AT+EGMR=1,7,"<IMEI>"
	cmd := fmt.Sprintf(`AT+EGMR=1,7,"%s"`, newImei)
	res, err := h.engine.Exec(cmd)
	if err != nil || !strings.Contains(res.Raw, "OK") {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("AT+EGMR write failed: %v", err))
		return
	}

	// Flag pending reboot in /tmp
	_ = os.WriteFile(imeiRebootPendingFlag, []byte(newImei), 0644)

	JSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"imei":            newImei,
		"detail":          "IMEI written to modem NVM. Reboot required to take effect.",
		"reboot_required": true,
	})
}

func (h *CellularImeiHandler) handleSaveBackup(w http.ResponseWriter, p ImeiSavePayload) {
	enabled := false
	if p.Enabled != nil {
		enabled = *p.Enabled
	}

	backupImei := strings.TrimSpace(p.BackupImei)
	if enabled {
		if backupImei == "" {
			Error(w, http.StatusBadRequest, "Backup IMEI is required when backup is enabled")
			return
		}
		if !ValidateLuhnIMEI(backupImei) {
			Error(w, http.StatusBadRequest, "Backup IMEI fails 15-digit Luhn validation")
			return
		}
	}

	cfg := BackupImeiConfig{
		Enabled: enabled,
		Imei:    backupImei,
	}

	if err := writeImeiBackupConfig(cfg); err != nil {
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save backup config: %v", err))
		return
	}

	Success(w, map[string]interface{}{
		"message": "Backup IMEI configuration saved",
	})
}

func (h *CellularImeiHandler) handleReboot(w http.ResponseWriter) {
	_ = os.Remove(imeiRebootPendingFlag)
	// Execute reboot asynchronously
	go func() {
		time.Sleep(1 * time.Second)
		_ = exec.Command("reboot").Run()
	}()

	Success(w, map[string]interface{}{
		"message": "Rebooting device...",
	})
}

// ValidateLuhnIMEI checks if the IMEI is exactly 15 digits and passes the Luhn (mod 10) algorithm.
func ValidateLuhnIMEI(imei string) bool {
	matched, _ := regexp.MatchString(`^[0-9]{15}$`, imei)
	if !matched {
		return false
	}

	sum := 0
	for i := 0; i < 15; i++ {
		digit := int(imei[i] - '0')
		if i%2 == 1 { // 2nd, 4th, 6th... (0-indexed 1, 3, 5...)
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}

	return sum%10 == 0
}

// CalculateLuhnCheckDigit computes the 15th digit for a 14-digit prefix.
func CalculateLuhnCheckDigit(prefix14 string) (int, bool) {
	if len(prefix14) != 14 || !isDigitsOnly(prefix14) {
		return 0, false
	}
	sum := 0
	for i := 0; i < 14; i++ {
		digit := int(prefix14[i] - '0')
		if i%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	checkDigit := (10 - (sum % 10)) % 10
	return checkDigit, true
}

func isDigitsOnly(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func readImeiBackupConfig() BackupImeiConfig {
	data, err := os.ReadFile(imeiBackupPath)
	if err != nil {
		return BackupImeiConfig{Enabled: false, Imei: ""}
	}
	var c BackupImeiConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return BackupImeiConfig{Enabled: false, Imei: ""}
	}
	return c
}

func writeImeiBackupConfig(c BackupImeiConfig) error {
	_ = os.MkdirAll(filepath.Dir(imeiBackupPath), 0755)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(imeiBackupPath, data, 0644)
}
