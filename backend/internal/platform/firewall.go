package platform

import (
	"log"
	"os/exec"
	"runtime"
)

// InitFirewallRules initializes firewall chains and rules to protect the modem on WAN interfaces.
// It drops ports 80 & 443 TCP from cellular WAN interfaces (rmnet+) while allowing trusted LAN/local/VPN traffic.
func InitFirewallRules() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	iptablesPath, err := exec.LookPath("iptables")
	if err != nil {
		log.Printf("[Firewall] iptables binary not found, skipping firewall init: %v", err)
		return nil
	}

	// 1. Create user-defined chain QMANAGER_FW if it doesn't exist
	_ = exec.Command(iptablesPath, "-N", "QMANAGER_FW").Run()

	// 2. Check if chain is already jumped from INPUT; insert if not present
	checkCmd := exec.Command(iptablesPath, "-C", "INPUT", "-j", "QMANAGER_FW")
	if err := checkCmd.Run(); err != nil {
		_ = exec.Command(iptablesPath, "-I", "INPUT", "1", "-j", "QMANAGER_FW").Run()
	}

	// 3. Flush chain QMANAGER_FW
	_ = exec.Command(iptablesPath, "-F", "QMANAGER_FW").Run()

	// 4. Add allow rules for local / trusted interfaces and established connections
	allowRules := [][]string{
		{"-A", "QMANAGER_FW", "-i", "lo", "-j", "ACCEPT"},
		{"-A", "QMANAGER_FW", "-i", "bridge0", "-j", "ACCEPT"},
		{"-A", "QMANAGER_FW", "-i", "eth0", "-j", "ACCEPT"},
		{"-A", "QMANAGER_FW", "-i", "usb+", "-j", "ACCEPT"},
		{"-A", "QMANAGER_FW", "-i", "tailscale0", "-j", "ACCEPT"},
		{"-A", "QMANAGER_FW", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
	}

	for _, rule := range allowRules {
		_ = exec.Command(iptablesPath, rule...).Run()
	}

	// 5. Add drop rules for web server ports (80 & 443 TCP) on cellular WAN (rmnet+)
	dropRules := [][]string{
		{"-A", "QMANAGER_FW", "-i", "rmnet+", "-p", "tcp", "--dport", "80", "-j", "DROP"},
		{"-A", "QMANAGER_FW", "-i", "rmnet+", "-p", "tcp", "--dport", "443", "-j", "DROP"},
	}

	for _, rule := range dropRules {
		_ = exec.Command(iptablesPath, rule...).Run()
	}

	// 6. Return rule
	_ = exec.Command(iptablesPath, "-A", "QMANAGER_FW", "-j", "RETURN").Run()

	log.Println("[Firewall] QManager WAN protection rules initialized successfully")
	return nil
}
