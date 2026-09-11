package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"qmanager/internal/atengine"
	"qmanager/internal/platform"
)

// SignalAntenna holds per-antenna signal metric (RSRP, RSRQ, SINR).
type SignalAntenna struct {
	RSRP *int `json:"rsrp"`
	RSRQ *int `json:"rsrq"`
	SINR *int `json:"sinr"`
}

// SignalPerAntenna holds per-antenna arrays.
type SignalPerAntenna struct {
	LteRSRP []*int `json:"lte_rsrp"`
	LteRSRQ []*int `json:"lte_rsrq"`
	LteSINR []*int `json:"lte_sinr"`
	NrRSRP  []*int `json:"nr_rsrp"`
	NrRSRQ  []*int `json:"nr_rsrq"`
	NrSINR  []*int `json:"nr_sinr"`
}

// SignalObject represents the nested signal metrics.
type SignalObject struct {
	RSRP       int              `json:"rsrp"`
	RSRQ       int              `json:"rsrq"`
	RSSI       int              `json:"rssi"`
	SINR       int              `json:"sinr"`
	CQI        int              `json:"cqi"`
	RXLev      int              `json:"rxlev"`
	PerAntenna SignalPerAntenna `json:"per_antenna"`
}

// NetworkObject represents active network details.
type NetworkObject struct {
	Registered         bool                        `json:"registered"`
	Roaming            bool                        `json:"roaming"`
	Tech               string                      `json:"tech"`
	Type               string                      `json:"type"`
	MCC                string                      `json:"mcc"`
	MNC                string                      `json:"mnc"`
	Carrier            string                      `json:"carrier"`
	Operator           string                      `json:"operator"`
	APN                string                      `json:"apn"`
	IPAddress          string                      `json:"ip_address"`
	IPv6Address        string                      `json:"ipv6_address"`
	WanIPv4            string                      `json:"wan_ipv4"`
	WanIPv6            string                      `json:"wan_ipv6"`
	DNSServers         []string                    `json:"dns_servers"`
	PrimaryDNS         string                      `json:"primary_dns"`
	SecondaryDNS       string                      `json:"secondary_dns"`
	CarrierComponents  []atengine.CarrierComponent `json:"carrier_components"`
	TotalBandwidthMHz  int                         `json:"total_bandwidth_mhz"`
	BandwidthDetails   string                      `json:"bandwidth_details"`
	CAActive           bool                        `json:"ca_active"`
	CACount            int                         `json:"ca_count"`
	NRCAActive         bool                        `json:"nr_ca_active"`
	NRCACount          int                         `json:"nr_ca_count"`
	ServiceStatus      string                      `json:"service_status"`
	CFUN               int                         `json:"cfun"`
	SimSlot            int                         `json:"sim_slot"`
}

// CellObject represents primary cell parameters.
type CellObject struct {
	CellID      string `json:"cell_id"`
	PCID        int    `json:"pcid"`
	EARFCN      int    `json:"earfcn"`
	ARFCN       int    `json:"arfcn"`
	Band        string `json:"band"`
	Bandwidth   string `json:"bandwidth"`
	ULBandwidth string `json:"ul_bandwidth"`
	DLBandwidth string `json:"dl_bandwidth"`
	TAC         string `json:"tac"`
	ENodeBID    int    `json:"enodeb_id"`
	SectorID    int    `json:"sector_id"`
	TA          int    `json:"ta"`
}

// LteStatusObject represents detailed LTE status.
type LteStatusObject struct {
	State     string `json:"state"`
	Band      string `json:"band"`
	EARFCN    *int   `json:"earfcn"`
	Bandwidth *int   `json:"bandwidth"`
	PCI       *int   `json:"pci"`
	CellID    *int   `json:"cell_id"`
	ENodeBID  *int   `json:"enodeb_id"`
	SectorID  *int   `json:"sector_id"`
	TAC       *int   `json:"tac"`
	RSRP      *int   `json:"rsrp"`
	RSRQ      *int   `json:"rsrq"`
	SINR      *int   `json:"sinr"`
	RSSI      *int   `json:"rssi"`
	TA        *int   `json:"ta"`
}

// NrStatusObject represents detailed NR status.
type NrStatusObject struct {
	State    string `json:"state"`
	Band     string `json:"band"`
	ARFCN    *int   `json:"arfcn"`
	PCI      *int   `json:"pci"`
	CellID   *int64 `json:"cell_id"`
	ENodeBID *int64 `json:"enodeb_id,omitempty"`
	GNodeBID *int64 `json:"gnodeb_id,omitempty"`
	SectorID *int64 `json:"sector_id"`
	TAC      *int   `json:"tac"`
	RSRP     *int   `json:"rsrp"`
	RSRQ     *int   `json:"rsrq"`
	SINR     *int   `json:"sinr"`
	SCS      *int   `json:"scs"`
	TA       *int   `json:"ta"`
}

// SecondarySignalsObject represents 5G NSA secondary cell signal metrics.
type SecondarySignalsObject struct {
	NR5GRSRP int    `json:"nr5g_rsrp"`
	NR5GRSRQ int    `json:"nr5g_rsrq"`
	NR5GSINR int    `json:"nr5g_sinr"`
	NR5GBand string `json:"nr5g_band"`
	NR5GARFCN int   `json:"nr5g_arfcn"`
	NR5GPCI  int    `json:"nr5g_pci"`
}

// SimObject represents SIM readiness and identities.
type SimObject struct {
	Status      string `json:"status"`
	Inserted    bool   `json:"inserted"`
	Slot        int    `json:"slot"`
	ICCID       string `json:"iccid"`
	IMSI        string `json:"imsi"`
	PhoneNumber string `json:"phone_number"`
	PINStatus   string `json:"pin_status"`
}

// TrafficObject represents network interface transfer counters and rates.
type TrafficObject struct {
	RXBytes    uint64  `json:"rx_bytes"`
	TXBytes    uint64  `json:"tx_bytes"`
	RXRateKbps float64 `json:"rx_rate_kbps"`
	TXRateKbps float64 `json:"tx_rate_kbps"`
}

// SystemObject represents host hardware metrics.
type SystemObject struct {
	CPUUsagePct   float64 `json:"cpu_usage_pct"`
	CPUUsage      float64 `json:"cpu_usage"`
	RAMTotalMB    float64 `json:"ram_total_mb"`
	RAMUsedMB     float64 `json:"ram_used_mb"`
	RAMFreeMB     float64 `json:"ram_free_mb"`
	RAMUsagePct   float64 `json:"ram_usage_pct"`
	TemperatureC  float64 `json:"temperature_c"`
	Temperature   float64 `json:"temperature"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	LoadAverage   string  `json:"load_average"`
}

// DeviceObject represents device hardware, firmware, and supported band identities.
type DeviceObject struct {
	Temperature           *float64 `json:"temperature"`
	CPUUsage              float64  `json:"cpu_usage"`
	MemoryUsedMB          float64  `json:"memory_used_mb"`
	MemoryTotalMB         float64  `json:"memory_total_mb"`
	UptimeSeconds         float64  `json:"uptime_seconds"`
	ConnUptimeSeconds     float64  `json:"conn_uptime_seconds"`
	LastConnUptimeSeconds float64  `json:"last_conn_uptime_seconds"`
	Firmware              string   `json:"firmware"`
	BuildDate             string   `json:"build_date"`
	Manufacturer          string   `json:"manufacturer"`
	Model                 string   `json:"model"`
	IMEI                  string   `json:"imei"`
	IMSI                  string   `json:"imsi"`
	ICCID                 string   `json:"iccid"`
	PhoneNumber           string   `json:"phone_number"`
	LTECategory           string   `json:"lte_category"`
	MIMO                  string   `json:"mimo"`
	SupportedLTEBands     string   `json:"supported_lte_bands"`
	SupportedNSABands     string   `json:"supported_nsa_nr5g_bands"`
	SupportedSABands      string   `json:"supported_sa_nr5g_bands"`
}

// ConnectivityObject represents live ping reachability and latency metrics.
type ConnectivityObject struct {
	InternetAvailable  *bool      `json:"internet_available"`
	Status             string     `json:"status"`
	LatencyMs          *float64   `json:"latency_ms"`
	AvgLatencyMs       *float64   `json:"avg_latency_ms"`
	MinLatencyMs       *float64   `json:"min_latency_ms"`
	MaxLatencyMs       *float64   `json:"max_latency_ms"`
	JitterMs           *float64   `json:"jitter_ms"`
	PacketLossPct      *float64   `json:"packet_loss_pct"`
	PingTarget         string     `json:"ping_target"`
	LatencyHistory     []*float64 `json:"latency_history"`
	HistoryIntervalSec int        `json:"history_interval_sec"`
	HistorySize        int        `json:"history_size"`
	LastFamily         string     `json:"last_family,omitempty"`
	Profile            string     `json:"profile,omitempty"`
}

// ModemStatus provides both flat legacy fields and structured nested blocks for complete frontend compatibility.
type ModemStatus struct {
	// Root flat fields
	Timestamp          int64                       `json:"timestamp"`
	ModemReachable     bool                        `json:"modem_reachable"`
	LastSuccessfulPoll int64                       `json:"last_successful_poll"`
	Errors             []string                    `json:"errors"`
	Online             bool                        `json:"online"`
	SystemState        string                      `json:"system_state"`
	Mode               string                      `json:"mode"`
	MCC                string                      `json:"mcc"`
	MNC                string                      `json:"mnc"`
	Operator           string                      `json:"operator"`
	Carrier            string                      `json:"carrier"`
	CellID             string                      `json:"cell_id"`
	PCID               int                         `json:"pcid"`
	EARFCN             int                         `json:"earfcn"`
	Band               string                      `json:"band"`
	RSRP               int                         `json:"rsrp"`
	RSRQ               int                         `json:"rsrq"`
	RSSI               int                         `json:"rssi"`
	SINR               int                         `json:"sinr"`
	CQI                int                         `json:"cqi"`
	CA                 []atengine.CarrierComponent `json:"carrier_aggregation"`
	DeviceModel        string                      `json:"device_model"`
	Revision           string                      `json:"revision"`
	Serial             string                      `json:"serial"`
	IMEI               string                      `json:"imei"`
	IMSI               string                      `json:"imsi"`
	ICCID              string                      `json:"iccid"`
	PhoneNumber        string                      `json:"phone_number"`

	// Nested sub-objects matching frontend/types/modem-status.ts
	Signal             SignalObject                `json:"signal"`
	Network            NetworkObject               `json:"network"`
	Cell               CellObject                  `json:"cell"`
	LTE                LteStatusObject             `json:"lte"`
	NR                 NrStatusObject              `json:"nr"`
	Device             DeviceObject                `json:"device"`
	Connectivity       ConnectivityObject          `json:"connectivity"`
	SignalPerAntenna   SignalPerAntenna            `json:"signal_per_antenna"`
	SecondarySignals   SecondarySignalsObject      `json:"secondary_signals"`
	SIM                SimObject                   `json:"sim"`
	Traffic            TrafficObject               `json:"traffic"`
	System             SystemObject                `json:"system"`
}

// Poller runs periodic telemetry updates.
type Poller struct {
	engine        *atengine.Engine
	identity      platform.Identity
	interval      time.Duration
	mu            sync.RWMutex
	current       *ModemStatus
	stopCh        chan struct{}
	running       bool
	connStartTime time.Time
	prober        *PingProber

	// State latches for change detection & event feed
	lastBand      string
	lastPCID      int
	lastMode      string
	lastCACount   int
	lastOnline    bool
	initEventSent bool

	// Cached identities
	imei              string
	imsi              string
	iccid             string
	phoneNumber       string
	carrier           string
	simStatus         string
	cfun              int
	lteTA             *int
	nrTA              *int
	supportedLTEBands string
	supportedNSABands string
	supportedSABands  string
	lastConnUptime    float64

	pollCount uint64
}

const (
	DefaultSupportedLTEBands = "1:2:3:4:5:7:8:12:13:14:17:18:19:20:25:26:28:29:30:32:34:38:39:40:41:42:43:46:48:66:71"
	DefaultSupportedNRBands  = "1:2:3:5:7:8:12:13:14:18:20:25:26:28:29:30:38:40:41:48:66:70:71:75:76:77:78:79"
)

// NewPoller initializes a new telemetry poller.
func NewPoller(eng *atengine.Engine, id platform.Identity, interval time.Duration) *Poller {
	if interval < 500*time.Millisecond {
		interval = 1 * time.Second
	}
	return &Poller{
		engine:            eng,
		identity:          id,
		interval:          interval,
		stopCh:            make(chan struct{}),
		current:           newDefaultStatus(id),
		simStatus:         "ready",
		cfun:              1,
		supportedLTEBands: DefaultSupportedLTEBands,
		supportedNSABands: DefaultSupportedNRBands,
		supportedSABands:  DefaultSupportedNRBands,
	}
}

func (p *Poller) SetProber(pr *PingProber) {
	p.mu.Lock()
	p.prober = pr
	p.mu.Unlock()
}

func newDefaultStatus(id platform.Identity) *ModemStatus {
	return &ModemStatus{
		Timestamp:          time.Now().Unix(),
		ModemReachable:     true,
		LastSuccessfulPoll: time.Now().Unix(),
		Errors:             []string{},
		Online:             false,
		SystemState:        "normal",
		DeviceModel:        id.Model,
		Revision:           id.Revision,
		Serial:             id.Serial,
		Device: DeviceObject{
			Model:             id.Model,
			Firmware:          id.Revision,
			Manufacturer:      "Quectel",
			SupportedLTEBands: DefaultSupportedLTEBands,
			SupportedNSABands: DefaultSupportedNRBands,
			SupportedSABands:  DefaultSupportedNRBands,
		},
		Network: NetworkObject{
			Type:              "LTE",
			Tech:              "LTE",
			ServiceStatus:     "searching",
			CFUN:              1,
			SimSlot:           1,
			CarrierComponents: []atengine.CarrierComponent{},
		},
		LTE: LteStatusObject{
			State: "searching",
		},
		NR: NrStatusObject{
			State: "disconnected",
		},
		SIM: SimObject{
			Status:   "ready",
			Inserted: true,
			Slot:     1,
		},
		Connectivity: ConnectivityObject{
			Status:             "connected",
			PingTarget:         "1.1.1.1:53",
			LatencyHistory:     make([]*float64, 0),
			HistoryIntervalSec: 2,
			HistorySize:        30,
			LastFamily:         "ipv4",
		},
		SignalPerAntenna: SignalPerAntenna{
			LteRSRP: make([]*int, 4),
			LteRSRQ: make([]*int, 4),
			LteSINR: make([]*int, 4),
			NrRSRP:  make([]*int, 4),
			NrRSRQ:  make([]*int, 4),
			NrSINR:  make([]*int, 4),
		},
	}
}

// Start begins background polling loop.
func (p *Poller) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

	go p.loop()
}

func (p *Poller) queryIdentities(ctx context.Context) {
	runCmd := func(cmd string) (*atengine.Result, error) {
		cmdCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return p.engine.ExecContext(cmdCtx, cmd)
	}

	_, _ = runCmd(`AT+QNWCFG="lte_time_advance",1`)
	_, _ = runCmd(`AT+QNWCFG="nr5g_time_advance",1`)

	if res, err := runCmd("AT+CFUN?"); err == nil {
		p.cfun = atengine.ParseCFUN(res.Raw)
	}

	if res, err := runCmd(`AT+QNWCFG="lte_time_advance"`); err == nil {
		if ta := atengine.ParseTimeAdvance(res.Raw, false); ta != nil {
			p.lteTA = ta
		}
	}
	if res, err := runCmd(`AT+QNWCFG="nr5g_time_advance"`); err == nil {
		if nta := atengine.ParseTimeAdvance(res.Raw, true); nta != nil {
			p.nrTA = nta
		}
	}

	if p.imei == "" {
		if res, err := runCmd("AT+CGSN"); err == nil {
			if imei := atengine.ParseCGSN(res.Raw); imei != "" {
				p.imei = imei
			}
		}
	}
	if p.imsi == "" {
		if res, err := runCmd("AT+CIMI"); err == nil {
			if imsi := atengine.ParseCIMI(res.Raw); imsi != "" {
				p.imsi = imsi
			}
		}
	}
	if p.iccid == "" {
		if res, err := runCmd("AT+QCCID"); err == nil {
			if iccid := atengine.ParseQCCID(res.Raw); iccid != "" {
				p.iccid = iccid
			}
		}
	}
	if res, err := runCmd("AT+CPIN?"); err == nil {
		pin := atengine.ParseCPIN(res.Raw)
		if strings.EqualFold(pin, "READY") {
			p.simStatus = "ready"
		} else if pin != "" {
			p.simStatus = strings.ToLower(pin)
		}
	}
	if res, err := runCmd("AT+COPS?"); err == nil {
		if cops := atengine.ParseCOPS(res.Raw); cops != "" {
			p.carrier = cops
		}
	}
	if p.supportedLTEBands == "" || p.supportedLTEBands == DefaultSupportedLTEBands {
		if res, err := runCmd(`AT+QNWPREFCFG="policy_band"`); err == nil {
			info := atengine.ParsePolicyBand(res.Raw)
			if info.LTEBands != "" {
				p.supportedLTEBands = info.LTEBands
			}
			if info.NSANR5GBands != "" {
				p.supportedNSABands = info.NSANR5GBands
			}
			if info.SANR5GBands != "" {
				p.supportedSABands = info.SANR5GBands
			}
		}
	}
}

// Stop terminates the polling loop.
func (p *Poller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	p.running = false
	close(p.stopCh)
}

func (p *Poller) loop() {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.poll()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.poll()
		}
	}
}

// GetStatus returns a snapshot copy of current modem status.
func (p *Poller) GetStatus() *ModemStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

func (p *Poller) poll() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.pollCount++
	if p.pollCount%15 == 0 {
		go p.queryIdentities(context.Background())
	}
	if p.pollCount%5 == 0 {
		if res, err := p.engine.ExecLow(ctx, `AT+QNWCFG="lte_time_advance"`); err == nil {
			if ta := atengine.ParseTimeAdvance(res.Raw, false); ta != nil {
				p.lteTA = ta
			}
		}
		if res, err := p.engine.ExecLow(ctx, `AT+QNWCFG="nr5g_time_advance"`); err == nil {
			if nta := atengine.ParseTimeAdvance(res.Raw, true); nta != nil {
				p.nrTA = nta
			}
		}
	}

	metrics := platform.GetSystemMetrics()
	totalMB := math.Round(float64(metrics.MemTotalKB) / 1024.0)
	usedMB := math.Round(float64(metrics.MemTotalKB-metrics.MemAvailKB) / 1024.0)
	freeMB := math.Round(float64(metrics.MemFreeKB) / 1024.0)
	cpuUsage := math.Round(metrics.CPUUsage)

	// Aggregate interface traffic
	var totalRx, totalTx uint64
	for _, iface := range metrics.Network {
		totalRx += iface.RxBytes
		totalTx += iface.TxBytes
	}

	wanIPv4, wanIPv6 := platform.GetInterfaceIP("rmnet_data0")
	if wanIPv4 == "" {
		wanIPv4, wanIPv6 = platform.GetInterfaceIP("rmnet_mhi0")
	}
	if wanIPv4 == "" {
		wanIPv4, wanIPv6 = platform.GetInterfaceIP("rmnet_ipa0")
	}

	status := &ModemStatus{
		Timestamp:          time.Now().Unix(),
		ModemReachable:     true,
		LastSuccessfulPoll: time.Now().Unix(),
		Errors:             []string{},
		Online:             false,
		SystemState:        "normal",
		DeviceModel:        p.identity.Model,
		Revision:           p.identity.Revision,
		Serial:             p.identity.Serial,
		IMEI:               p.imei,
		IMSI:               p.imsi,
		ICCID:              p.iccid,
		PhoneNumber:        p.phoneNumber,
		Carrier:            p.carrier,
		Operator:           p.carrier,
		Device: DeviceObject{
			Model:                 p.identity.Model,
			Firmware:              p.identity.Revision,
			BuildDate:             p.identity.PackageTime,
			Manufacturer:          "Quectel",
			IMEI:                  p.imei,
			IMSI:                  p.imsi,
			ICCID:                 p.iccid,
			PhoneNumber:           p.phoneNumber,
			CPUUsage:              cpuUsage,
			MemoryTotalMB:         totalMB,
			MemoryUsedMB:          usedMB,
			UptimeSeconds:         metrics.UptimeSeconds,
			LastConnUptimeSeconds: p.lastConnUptime,
			Temperature:           &metrics.CpuTempC,
			SupportedLTEBands:     p.supportedLTEBands,
			SupportedNSABands:     p.supportedNSABands,
			SupportedSABands:      p.supportedSABands,
		},
		System: SystemObject{
			CPUUsagePct:   cpuUsage,
			CPUUsage:      cpuUsage,
			RAMTotalMB:    totalMB,
			RAMUsedMB:     usedMB,
			RAMFreeMB:     freeMB,
			RAMUsagePct:   metrics.MemUsagePct,
			TemperatureC:  metrics.CpuTempC,
			Temperature:   metrics.CpuTempC,
			UptimeSeconds: metrics.UptimeSeconds,
		},
		Traffic: TrafficObject{
			RXBytes: totalRx,
			TXBytes: totalTx,
		},
		SIM: SimObject{
			Status:   p.simStatus,
			Inserted: p.simStatus == "ready",
			Slot:     1,
			ICCID:    p.iccid,
			IMSI:     p.imsi,
		},
		Connectivity: ConnectivityObject{
			Status: "connected",
		},
		SignalPerAntenna: SignalPerAntenna{
			LteRSRP: make([]*int, 4),
			LteRSRQ: make([]*int, 4),
			LteSINR: make([]*int, 4),
			NrRSRP:  make([]*int, 4),
			NrRSRQ:  make([]*int, 4),
			NrSINR:  make([]*int, 4),
		},
		LTE: LteStatusObject{
			State: "disconnected",
			TA:    p.lteTA,
		},
		NR: NrStatusObject{
			State: "disconnected",
			TA:    p.nrTA,
		},
		Network: NetworkObject{
			Type:              "LTE",
			Tech:              "LTE",
			ServiceStatus:     "no_service",
			Carrier:           p.carrier,
			Operator:          p.carrier,
			SimSlot:           1,
			CFUN:              p.cfun,
			WanIPv4:           wanIPv4,
			WanIPv6:           wanIPv6,
			IPAddress:         wanIPv4,
			IPv6Address:       wanIPv6,
			CarrierComponents: []atengine.CarrierComponent{},
		},
	}

	// 1. Serving cell info
	if res, err := p.engine.ExecLow(ctx, `AT+QENG="servingcell"`); err == nil {
		if cell := atengine.ParseQENGServingCell(res.Raw); cell != nil {
			status.Online = cell.State == "CONNECT" || cell.State == "NOCONN"
			status.Mode = cell.Mode
			status.MCC = cell.MCC
			status.MNC = cell.MNC
			status.CellID = cell.CellID
			status.PCID = cell.PCID
			status.EARFCN = cell.EARFCN
			status.Band = cell.Band
			status.RSRP = cell.RSRP
			status.RSRQ = cell.RSRQ
			status.RSSI = cell.RSSI
			status.SINR = cell.SINR
			status.CQI = cell.CQI

			// Populate nested signal
			status.Signal = SignalObject{
				RSRP:  cell.RSRP,
				RSRQ:  cell.RSRQ,
				RSSI:  cell.RSSI,
				SINR:  cell.SINR,
				CQI:   cell.CQI,
				RXLev: cell.RSRP,
			}

			// Connection state string according to TypeScript ConnectionState
			connState := "disconnected"
			if status.Online {
				connState = "connected"
			} else if cell.State == "SEARCH" {
				connState = "searching"
			} else if cell.State == "LIMSRV" {
				connState = "limited"
			}

			// Track connection uptime
			if status.Online {
				if p.connStartTime.IsZero() {
					p.connStartTime = time.Now()
				}
				status.Device.ConnUptimeSeconds = time.Since(p.connStartTime).Seconds()
			} else {
				if !p.connStartTime.IsZero() {
					p.lastConnUptime = time.Since(p.connStartTime).Seconds()
				}
				p.connStartTime = time.Time{}
				status.Device.ConnUptimeSeconds = 0
			}
			status.Device.LastConnUptimeSeconds = p.lastConnUptime

			// Populate nested cell
			cid64, err := strconv.ParseInt(cell.CellID, 16, 64)
			if err != nil {
				cid64, _ = strconv.ParseInt(cell.CellID, 10, 64)
			}
			var nodeb64, sector64 int64
			if strings.HasPrefix(cell.Mode, "NR5G") {
				nodeb64 = cid64 >> 14     // 5G gNodeB
				sector64 = cid64 & 0x3FFF // 5G Sector
			} else {
				nodeb64 = cid64 >> 8      // LTE eNodeB
				sector64 = cid64 & 0xFF   // LTE Sector
			}

			status.Cell = CellObject{
				CellID:    cell.CellID,
				PCID:      cell.PCID,
				EARFCN:    cell.EARFCN,
				Band:      cell.Band,
				Bandwidth: cell.Bandwidth,
				ENodeBID:  int(nodeb64),
				SectorID:  int(sector64),
			}

			// Populate LTE status object
			pciVal := cell.PCID
			earfcnVal := cell.EARFCN
			cellIdInt := int(cid64)
			enodebInt := int(nodeb64)
			sectorInt := int(sector64)
			rsrpVal := cell.RSRP
			rsrqVal := cell.RSRQ
			sinrVal := cell.SINR
			rssiVal := cell.RSSI

			var tacInt *int
			if cell.TAC != "" {
				if t64, err := strconv.ParseInt(cell.TAC, 16, 64); err == nil {
					tVal := int(t64)
					tacInt = &tVal
				}
			}

			var bwInt *int
			if cell.Bandwidth != "" {
				bwStr := strings.TrimSuffix(cell.Bandwidth, "M")
				if b, err := strconv.Atoi(bwStr); err == nil {
					bwInt = &b
				}
			}

			status.LTE = LteStatusObject{
				State:     connState,
				Band:      cell.Band,
				EARFCN:    &earfcnVal,
				Bandwidth: bwInt,
				PCI:       &pciVal,
				CellID:    &cellIdInt,
				ENodeBID:  &enodebInt,
				SectorID:  &sectorInt,
				TAC:       tacInt,
				RSRP:      &rsrpVal,
				RSRQ:      &rsrqVal,
				SINR:      &sinrVal,
				RSSI:      &rssiVal,
				TA:        p.lteTA,
			}

			// Populate NR status object
			if strings.HasPrefix(cell.Mode, "NR5G-SA") {
				status.NR = NrStatusObject{
					State:    connState,
					Band:     cell.Band,
					ARFCN:    &earfcnVal,
					PCI:      &pciVal,
					CellID:   &cid64,
					ENodeBID: &nodeb64,
					GNodeBID: &nodeb64,
					SectorID: &sector64,
					RSRP:     &rsrpVal,
					RSRQ:     &rsrqVal,
					SINR:     &sinrVal,
					TA:       p.nrTA,
				}
			} else if cell.HasNR5GNSA {
				nrPci := cell.NR5GPCI
				nrArfcn := cell.NR5GARFCN
				nrRsrp := cell.NR5GRSRP
				nrRsrq := cell.NR5GRSRQ
				nrSinr := cell.NR5GSINR
				var scsInt int
				if cell.NR5GSCS == "30kHz" {
					scsInt = 30
				} else if cell.NR5GSCS == "15kHz" {
					scsInt = 15
				} else if cell.NR5GSCS == "60kHz" {
					scsInt = 60
				} else if cell.NR5GSCS == "120kHz" {
					scsInt = 120
				}
				var scsPtr *int
				if scsInt > 0 {
					scsPtr = &scsInt
				}

				status.NR = NrStatusObject{
					State: connState,
					Band:  cell.NR5GBand,
					ARFCN: &nrArfcn,
					PCI:   &nrPci,
					RSRP:  &nrRsrp,
					RSRQ:  &nrRsrq,
					SINR:  &nrSinr,
					SCS:   scsPtr,
					TA:    p.nrTA,
				}
			}

			// Determine frontend NetworkType ("LTE", "5G-NSA", "5G-SA")
			netType := "LTE"
			if cell.Mode == "NR5G-NSA" || cell.HasNR5GNSA {
				netType = "5G-NSA"
			} else if cell.Mode == "NR5G-SA" {
				netType = "5G-SA"
			}

			// Service status
			serviceStatus := "no_service"
			if status.Online {
				serviceStatus = "connected"
			} else if cell.State == "SEARCH" {
				serviceStatus = "searching"
			} else if cell.State == "LIMSRV" {
				serviceStatus = "limited"
			}

			status.Network.Type = netType
			status.Network.Tech = netType
			status.Network.MCC = cell.MCC
			status.Network.MNC = cell.MNC
			status.Network.Registered = status.Online
			status.Network.ServiceStatus = serviceStatus
		}
	}

	// Micro-sleep between consecutive low-priority AT commands in 1Hz cycle
	time.Sleep(15 * time.Millisecond)

	// 2. Carrier aggregation
	if res, err := p.engine.ExecLow(ctx, `AT+QCAINFO`); err == nil {
		caList := atengine.ParseQCAINFO(res.Raw)

		// Reconcile NR leg in NSA EN-DC mode if AT+QCAINFO reports 0 dBm or invalid PCI (-99 / -100)
		if status.NR.PCI != nil && *status.NR.PCI > 0 {
			for i, comp := range caList {
				if comp.Technology == "NR" || strings.HasPrefix(strings.ToUpper(comp.Band), "N") {
					if caList[i].PCI <= 0 && *status.NR.PCI > 0 {
						caList[i].PCI = *status.NR.PCI
						caList[i].PCID = *status.NR.PCI
					}
					if caList[i].RSRP == 0 && status.NR.RSRP != nil && *status.NR.RSRP != 0 {
						caList[i].RSRP = *status.NR.RSRP
					}
					if caList[i].RSRQ == 0 && status.NR.RSRQ != nil && *status.NR.RSRQ != 0 {
						caList[i].RSRQ = *status.NR.RSRQ
					}
					if caList[i].SINR == 0 && status.NR.SINR != nil && *status.NR.SINR != 0 {
						caList[i].SINR = *status.NR.SINR
					}
				}
			}
		}

		status.CA = caList
		status.Network.CarrierComponents = caList
		status.Network.CACount = len(caList)
		status.Network.CAActive = len(caList) > 1

		nrCaCount := 0
		totalBW := 0
		var bwDetails []string
		for _, comp := range caList {
			totalBW += comp.BandwidthMHz
			if comp.Technology == "NR" || strings.HasPrefix(strings.ToUpper(comp.Band), "N") {
				nrCaCount++
			}
			if comp.Band != "" && comp.BandwidthMHz > 0 {
				bwDetails = append(bwDetails, comp.Band+": "+strconv.Itoa(comp.BandwidthMHz)+" MHz")
			}
		}
		status.Network.TotalBandwidthMHz = totalBW
		status.Network.BandwidthDetails = strings.Join(bwDetails, " + ")
		status.Network.NRCAActive = nrCaCount > 0
		status.Network.NRCACount = nrCaCount
	}

	time.Sleep(15 * time.Millisecond)

	// 3. Per-antenna signal metrics (AT+QRSRP, AT+QRSRQ, AT+QSINR)
	if res, err := p.engine.ExecLow(ctx, `AT+QRSRP`); err == nil {
		lte, nr := atengine.ParseAntennaSignals(res.Raw, "QRSRP")
		if len(lte) > 0 {
			status.SignalPerAntenna.LteRSRP = lte
		}
		if len(nr) > 0 {
			status.SignalPerAntenna.NrRSRP = nr
		}
	}
	if res, err := p.engine.ExecLow(ctx, `AT+QRSRQ`); err == nil {
		lte, nr := atengine.ParseAntennaSignals(res.Raw, "QRSRQ")
		if len(lte) > 0 {
			status.SignalPerAntenna.LteRSRQ = lte
		}
		if len(nr) > 0 {
			status.SignalPerAntenna.NrRSRQ = nr
		}
	}
	if res, err := p.engine.ExecLow(ctx, `AT+QSINR`); err == nil {
		lte, nr := atengine.ParseAntennaSignals(res.Raw, "QSINR")
		if len(lte) > 0 {
			status.SignalPerAntenna.LteSINR = lte
		}
		if len(nr) > 0 {
			status.SignalPerAntenna.NrSINR = nr
		}
	}

	// 4. Signal CSQ fallback if RSSI not parsed
	if status.RSSI == 0 {
		if res, err := p.engine.ExecLow(ctx, `AT+CSQ`); err == nil {
			if csq := atengine.ParseCSQ(res.Raw); csq != nil {
				status.RSSI = csq.RSRPDbm
				status.Signal.RSSI = csq.RSRPDbm
			}
		}
	}

	// 5. Populate live Ping / Connectivity metrics
	p.mu.RLock()
	prober := p.prober
	p.mu.RUnlock()

	if prober != nil {
		stats := prober.GetStats()
		avail := stats.LossPct < 100.0 && len(stats.RecentPoints) > 0
		status.Connectivity.InternetAvailable = &avail
		if avail {
			status.Connectivity.Status = "connected"
		} else if len(stats.RecentPoints) > 0 {
			status.Connectivity.Status = "disconnected"
		} else {
			status.Connectivity.Status = "unknown"
		}
		status.Connectivity.PingTarget = stats.Target
		status.Connectivity.HistoryIntervalSec = int(prober.interval.Seconds())
		if status.Connectivity.HistoryIntervalSec <= 0 {
			status.Connectivity.HistoryIntervalSec = 2
		}
		status.Connectivity.HistorySize = 30
		status.Connectivity.LastFamily = "ipv4"
		if stats.CurrentMs > 0 {
			status.Connectivity.LatencyMs = &stats.CurrentMs
		}
		if stats.AvgMs > 0 {
			status.Connectivity.AvgLatencyMs = &stats.AvgMs
		}
		if stats.MinMs > 0 {
			status.Connectivity.MinLatencyMs = &stats.MinMs
		}
		if stats.MaxMs > 0 {
			status.Connectivity.MaxLatencyMs = &stats.MaxMs
		}
		if stats.JitterMs > 0 {
			status.Connectivity.JitterMs = &stats.JitterMs
		}
		status.Connectivity.PacketLossPct = &stats.LossPct

		historySlice := make([]*float64, 0, len(stats.RecentPoints))
		for _, pt := range stats.RecentPoints {
			if pt.Success {
				lat := pt.LatencyMs
				historySlice = append(historySlice, &lat)
			} else {
				historySlice = append(historySlice, nil)
			}
		}
		status.Connectivity.LatencyHistory = historySlice
	}

	// 6. Change-detection latch for Network Events feed
	p.mu.Lock()
	if !p.initEventSent {
		p.initEventSent = true
		p.lastBand = status.Band
		p.lastPCID = status.PCID
		p.lastMode = status.Mode
		p.lastCACount = status.Network.CACount
		p.lastOnline = status.Online

		carrierName := status.Carrier
		if carrierName == "" {
			carrierName = status.Operator
		}
		if carrierName == "" {
			carrierName = "Cellular"
		}
		GetGlobalHistory().RecordEvent(NetworkEventItem{
			Timestamp: status.Timestamp,
			Type:      "link_up",
			Message:   fmt.Sprintf("QManager Telemetry online — %s (%s)", carrierName, status.Mode),
			Severity:  "info",
		})
	} else {
		// Band Change
		if status.Band != "" && p.lastBand != "" && status.Band != p.lastBand {
			GetGlobalHistory().RecordEvent(NetworkEventItem{
				Timestamp: status.Timestamp,
				Type:      "band_change",
				Message:   fmt.Sprintf("Band switched: %s → %s", p.lastBand, status.Band),
				Severity:  "info",
			})
			p.lastBand = status.Band
		} else if status.Band != "" && p.lastBand == "" {
			p.lastBand = status.Band
		}

		// PCI / Tower Handoff
		if status.PCID > 0 && p.lastPCID > 0 && status.PCID != p.lastPCID {
			GetGlobalHistory().RecordEvent(NetworkEventItem{
				Timestamp: status.Timestamp,
				Type:      "pci_change",
				Message:   fmt.Sprintf("Tower handoff: PCI %d → %d", p.lastPCID, status.PCID),
				Severity:  "info",
			})
			p.lastPCID = status.PCID
		} else if status.PCID > 0 && p.lastPCID == 0 {
			p.lastPCID = status.PCID
		}

		// Network Mode Change (e.g. LTE -> 5G-NSA / 5G-SA)
		if status.Mode != "" && p.lastMode != "" && status.Mode != p.lastMode {
			GetGlobalHistory().RecordEvent(NetworkEventItem{
				Timestamp: status.Timestamp,
				Type:      "network_mode",
				Message:   fmt.Sprintf("Network mode changed: %s → %s", p.lastMode, status.Mode),
				Severity:  "info",
			})
			p.lastMode = status.Mode
		} else if status.Mode != "" && p.lastMode == "" {
			p.lastMode = status.Mode
		}

		// Carrier Aggregation Change
		if status.Network.CACount != p.lastCACount {
			if status.Network.CACount > 1 && p.lastCACount <= 1 {
				GetGlobalHistory().RecordEvent(NetworkEventItem{
					Timestamp: status.Timestamp,
					Type:      "ca_change",
					Message:   fmt.Sprintf("Carrier Aggregation activated (%d carriers: %s)", status.Network.CACount, status.Network.BandwidthDetails),
					Severity:  "info",
				})
			} else if status.Network.CACount <= 1 && p.lastCACount > 1 {
				GetGlobalHistory().RecordEvent(NetworkEventItem{
					Timestamp: status.Timestamp,
					Type:      "ca_change",
					Message:   "Carrier Aggregation deactivated",
					Severity:  "warning",
				})
			}
			p.lastCACount = status.Network.CACount
		}

		// Signal Lost / Restored
		if p.lastOnline != status.Online {
			if !status.Online {
				GetGlobalHistory().RecordEvent(NetworkEventItem{
					Timestamp: status.Timestamp,
					Type:      "signal_lost",
					Message:   "Modem cellular connection lost",
					Severity:  "error",
				})
			} else {
				GetGlobalHistory().RecordEvent(NetworkEventItem{
					Timestamp: status.Timestamp,
					Type:      "signal_restored",
					Message:   "Modem cellular connection restored",
					Severity:  "info",
				})
			}
			p.lastOnline = status.Online
		}
	}

	p.current = status
	p.mu.Unlock()

	// Record in-memory signal history sample (Zero Flash Wear)
	var lteRsrp, lteRsrq, lteSinr []*int
	if status.LTE.RSRP != nil {
		lteRsrp = []*int{status.LTE.RSRP, nil, nil, nil}
	} else {
		lteRsrp = []*int{nil, nil, nil, nil}
	}
	if status.LTE.RSRQ != nil {
		lteRsrq = []*int{status.LTE.RSRQ, nil, nil, nil}
	} else {
		lteRsrq = []*int{nil, nil, nil, nil}
	}
	if status.LTE.SINR != nil {
		lteSinr = []*int{status.LTE.SINR, nil, nil, nil}
	} else {
		lteSinr = []*int{nil, nil, nil, nil}
	}

	var nrRsrp, nrRsrq, nrSinr []*int
	if status.NR.RSRP != nil {
		nrRsrp = []*int{status.NR.RSRP, nil, nil, nil}
	} else {
		nrRsrp = []*int{nil, nil, nil, nil}
	}
	if status.NR.RSRQ != nil {
		nrRsrq = []*int{status.NR.RSRQ, nil, nil, nil}
	} else {
		nrRsrq = []*int{nil, nil, nil, nil}
	}
	if status.NR.SINR != nil {
		nrSinr = []*int{status.NR.SINR, nil, nil, nil}
	} else {
		nrSinr = []*int{nil, nil, nil, nil}
	}

	GetGlobalHistory().RecordSignal(SignalHistoryPoint{
		TS:      status.Timestamp,
		LteRsrp: lteRsrp,
		LteRsrq: lteRsrq,
		LteSinr: lteSinr,
		NrRsrp:  nrRsrp,
		NrRsrq:  nrRsrq,
		NrSinr:  nrSinr,
	})

	_ = writeStatusFile("/tmp/qmanager_status.json", status)
}

func writeStatusFile(path string, status *ModemStatus) error {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
