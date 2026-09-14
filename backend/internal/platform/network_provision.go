package platform

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"qmanager/internal/config"
)

const (
	defaultDnsmasqConf = "/etc/dnsmasq.conf"
	defaultDnsmasqUnit = "/lib/systemd/system/dnsmasq.service"
)

// NetworkProvisioner coordinates zero-touch LAN setup and self-healing bridge on Quectel devices.
type NetworkProvisioner struct {
	cfgMgr  *config.Manager
	mu      sync.Mutex
	stopCh  chan struct{}
	running bool
}

// NewNetworkProvisioner creates a new network provisioning manager.
func NewNetworkProvisioner(cfgMgr *config.Manager) *NetworkProvisioner {
	return &NetworkProvisioner{
		cfgMgr: cfgMgr,
		stopCh: make(chan struct{}),
	}
}

// Start boots the background self-healing provisioner loop.
func (np *NetworkProvisioner) Start() {
	np.mu.Lock()
	if np.running {
		np.mu.Unlock()
		return
	}
	np.running = true
	np.stopCh = make(chan struct{})
	np.mu.Unlock()

	// Initial immediate provision
	np.ProvisionOnce()

	// Background health poller (checks every 15 seconds)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-np.stopCh:
				return
			case <-ticker.C:
				np.ProvisionOnce()
			}
		}
	}()
}

// Stop terminates the background loop.
func (np *NetworkProvisioner) Stop() {
	np.mu.Lock()
	defer np.mu.Unlock()
	if !np.running {
		return
	}
	close(np.stopCh)
	np.running = false
}

// ProvisionOnce checks interface states and configures bridge0 + dnsmasq if uninitialized.
func (np *NetworkProvisioner) ProvisionOnce() {
	c := np.cfgMgr.Get()
	// Default enabled if unconfigured
	if c.Network.AutoProvisionLAN == 0 && c.Network.GatewayIP != "" {
		return
	}

	gwIP := c.Network.GatewayIP
	if gwIP == "" {
		gwIP = "192.168.225.1"
	}
	mask := c.Network.SubnetMask
	if mask == "" {
		mask = "255.255.255.0"
	}
	dhcpStart := c.Network.DHCPStart
	if dhcpStart == "" {
		dhcpStart = "192.168.225.20"
	}
	dhcpEnd := c.Network.DHCPEnd
	if dhcpEnd == "" {
		dhcpEnd = "192.168.225.100"
	}
	leaseTime := c.Network.DHCPLeaseTime
	if leaseTime == "" {
		leaseTime = "12h"
	}

	// 1. Check if eth0 is present in sysfs
	if _, err := os.Stat("/sys/class/net/eth0"); err != nil {
		// eth0 is not present yet (PCIe not initialized or no eth port)
		return
	}

	// 2. Ensure bridge0 exists
	if _, err := os.Stat("/sys/class/net/bridge0"); err != nil {
		log.Println("🌐 [LAN-Provision] Creating bridge0...")
		_ = exec.Command("ip", "link", "add", "name", "bridge0", "type", "bridge").Run()
		_ = exec.Command("brctl", "addbr", "bridge0").Run()
	}

	// 3. Ensure eth0 is enrolled in bridge0
	isMaster := false
	if _, err := os.Stat("/sys/class/net/eth0/master"); err == nil {
		isMaster = true
	} else if _, err := os.Stat("/sys/class/net/bridge0/brif/eth0"); err == nil {
		isMaster = true
	}

	if !isMaster {
		log.Println("🌐 [LAN-Provision] Enrolling eth0 into bridge0...")
		_ = exec.Command("ip", "link", "set", "eth0", "master", "bridge0").Run()
		_ = exec.Command("brctl", "addif", "bridge0", "eth0").Run()
	}

	// 4. Ensure eth0 and bridge0 are UP
	_ = exec.Command("ip", "link", "set", "eth0", "up").Run()
	_ = exec.Command("ip", "link", "set", "bridge0", "up").Run()

	// 5. Clean Link-Local 169.254.x.x from eth0 if present
	flushLinkLocal("eth0")

	// 6. Ensure Gateway IP on bridge0
	hasIP, currentIP := hasAssignedIP("bridge0", gwIP)
	if !hasIP {
		log.Printf("🌐 [LAN-Provision] Assigning Gateway IP %s/24 to bridge0 (current: %s)\n", gwIP, currentIP)
		_ = exec.Command("ip", "addr", "add", gwIP+"/24", "brd", "+", "dev", "bridge0").Run()
		_ = exec.Command("ifconfig", "bridge0", gwIP, "netmask", mask, "up").Run()
	}

	// 7. Ensure /etc/dnsmasq.conf is present and properly configured
	ensureDnsmasqConfig(gwIP, mask, dhcpStart, dhcpEnd, leaseTime)

	// 8. Ensure dnsmasq service is active
	ensureDnsmasqService()

	// 9. Ensure QCMAP WWAN backhaul preference
	ensureQCMAPWWANBackhaul()
}

// hasAssignedIP checks if an interface holds the target IPv4.
func hasAssignedIP(ifaceName, targetIP string) (bool, string) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return false, ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return false, ""
	}
	var current string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && ipNet.IP.To4() != nil {
			ipStr := ipNet.IP.String()
			if ipStr == targetIP {
				return true, ipStr
			}
			current = ipStr
		}
	}
	return false, current
}

// flushLinkLocal removes 169.254.x.x addresses assigned by DHCP auto-IP.
func flushLinkLocal(ifaceName string) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && ipNet.IP.To4() != nil {
			if strings.HasPrefix(ipNet.IP.String(), "169.254.") {
				_ = exec.Command("ip", "addr", "del", ipNet.String(), "dev", ifaceName).Run()
			}
		}
	}
}

// ensureDnsmasqConfig updates /etc/dnsmasq.conf if missing or parameters changed.
func ensureDnsmasqConfig(gwIP, mask, dhcpStart, dhcpEnd, leaseTime string) {
	confContent := fmt.Sprintf(`# QManager Auto-Provisioned DNS & DHCP Config
interface=bridge0
bind-interfaces
dhcp-range=%s,%s,%s,%s
dhcp-option=option:router,%s
dhcp-option=option:dns-server,%s
domain-needed
bogus-priv
`, dhcpStart, dhcpEnd, mask, leaseTime, gwIP, gwIP)

	existing, err := os.ReadFile(defaultDnsmasqConf)
	if err == nil && strings.TrimSpace(string(existing)) == strings.TrimSpace(confContent) {
		return
	}

	// Write new configuration safely
	_ = os.WriteFile(defaultDnsmasqConf, []byte(confContent), 0644)
	// Restart dnsmasq to apply updated range
	_ = exec.Command("systemctl", "restart", "dnsmasq").Run()
}

// ensureDnsmasqService checks systemd and starts dnsmasq if inactive.
func ensureDnsmasqService() {
	unitContent := `[Unit]
Description=DNS and DHCP Server
After=sysinit.target bridge-eth0.service
DefaultDependencies=no

[Service]
Type=simple
ExecStart=/usr/bin/dnsmasq -k --conf-file=/etc/dnsmasq.conf
Restart=always
RestartSec=2s

[Install]
WantedBy=sysinit.target basic.target multi-user.target
`
	if _, err := os.Stat(defaultDnsmasqUnit); os.IsNotExist(err) {
		_ = os.WriteFile(defaultDnsmasqUnit, []byte(unitContent), 0644)
		_ = exec.Command("systemctl", "daemon-reload").Run()
		_ = exec.Command("systemctl", "enable", "dnsmasq").Run()
	}

	out, err := exec.Command("systemctl", "is-active", "dnsmasq").Output()
	if err != nil || strings.TrimSpace(string(out)) != "active" {
		_ = exec.Command("systemctl", "restart", "dnsmasq").Run()
	}
}

// ensureQCMAPWWANBackhaul fixes mobileap_cfg.xml so cellular data flows to LAN.
func ensureQCMAPWWANBackhaul() {
	paths := []string{"/etc/data/mobileap_cfg.xml", "/data/mobileap_cfg.xml"}
	for _, p := range paths {
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := string(content)
		if strings.Contains(s, "<FirstPreferredBackhaul>wwan</FirstPreferredBackhaul>") {
			continue
		}
		// Replace bt-pan or eth with wwan
		s = strings.ReplaceAll(s, "<FirstPreferredBackhaul>bt-pan</FirstPreferredBackhaul>", "<FirstPreferredBackhaul>wwan</FirstPreferredBackhaul>")
		s = strings.ReplaceAll(s, "<FirstPreferredBackhaul>eth</FirstPreferredBackhaul>", "<FirstPreferredBackhaul>wwan</FirstPreferredBackhaul>")
		if s != string(content) {
			_ = os.WriteFile(p, []byte(s), 0644)
		}
	}
}
