package telemetry

import (
	"context"
	"encoding/json"
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
	RSRP []int `json:"rsrp"`
	RSRQ []int `json:"rsrq"`
	SINR []int `json:"sinr"`
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
	CellID   *int   `json:"cell_id"`
	ENodeBID *int   `json:"enodeb_id"`
	SectorID *int   `json:"sector_id"`
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
	Temperature        *float64 `json:"temperature"`
	CPUUsage           float64  `json:"cpu_usage"`
	MemoryUsedMB       float64  `json:"memory_used_mb"`
	MemoryTotalMB      float64  `json:"memory_total_mb"`
	UptimeSeconds      float64  `json:"uptime_seconds"`
	ConnUptimeSeconds  float64  `json:"conn_uptime_seconds"`
	Firmware           string   `json:"firmware"`
	BuildDate          string   `json:"build_date"`
	Manufacturer       string   `json:"manufacturer"`
	Model              string   `json:"model"`
	IMEI               string   `json:"imei"`
	IMSI               string   `json:"imsi"`
	ICCID              string   `json:"iccid"`
	PhoneNumber        string   `json:"phone_number"`
	LTECategory        string   `json:"lte_category"`
	MIMO               string   `json:"mimo"`
	SupportedLTEBands  string   `json:"supported_lte_bands"`
	SupportedNSABands  string   `json:"supported_nsa_nr5g_bands"`
	SupportedSABands   string   `json:"supported_sa_nr5g_bands"`
}

// ConnectivityObject represents live ping reachability and latency metrics.
type ConnectivityObject struct {
	InternetAvailable *bool    `json:"internet_available"`
	Status            string   `json:"status"`
	LatencyMs         *float64 `json:"latency_ms"`
	AvgLatencyMs      *float64 `json:"avg_latency_ms"`
	JitterMs          *float64 `json:"jitter_ms"`
	PacketLossPct     *float64 `json:"packet_loss_pct"`
}

// ModemStatus provides both flat legacy fields and structured nested blocks for complete frontend compatibility.
type ModemStatus struct {
	// Root flat fields
	Timestamp          int64                       `json:"timestamp"`
	Online             bool                        `json:"online"`
	SystemState        string                      `json:"system_state"`
	Mode               string                      `json:"mode"`
	MCC                string                      `json:"mcc"`
	MNC                string                      `json:"mnc"`
	Operator           string                      `json:"operator"`
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
	engine   *atengine.Engine
	identity platform.Identity
	interval time.Duration
	mu       sync.RWMutex
	current  *ModemStatus
	stopCh   chan struct{}
	running  bool
}

// NewPoller initializes a new telemetry poller.
func NewPoller(eng *atengine.Engine, id platform.Identity, interval time.Duration) *Poller {
	if interval < 500*time.Millisecond {
		interval = 1 * time.Second
	}
	return &Poller{
		engine:   eng,
		identity: id,
		interval: interval,
		stopCh:   make(chan struct{}),
		current:  newDefaultStatus(id),
	}
}

func newDefaultStatus(id platform.Identity) *ModemStatus {
	return &ModemStatus{
		Timestamp:   time.Now().Unix(),
		Online:      false,
		SystemState: "normal",
		DeviceModel: id.Model,
		Revision:    id.Revision,
		Serial:      id.Serial,
		Device: DeviceObject{
			Model:        id.Model,
			Firmware:     id.Revision,
			Manufacturer: "Quectel",
		},
		Network: NetworkObject{
			Type:          "LTE",
			ServiceStatus: "no_service",
			CarrierComponents: []atengine.CarrierComponent{},
		},
		LTE: LteStatusObject{
			State: "disconnected",
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
			Status: "unknown",
		},
		SignalPerAntenna: SignalPerAntenna{
			RSRP: []int{},
			RSRQ: []int{},
			SINR: []int{},
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

	metrics := platform.GetSystemMetrics()
	totalMB := float64(metrics.MemTotalKB) / 1024.0
	usedMB := float64(metrics.MemTotalKB-metrics.MemAvailKB) / 1024.0
	freeMB := float64(metrics.MemFreeKB) / 1024.0

	// Aggregate interface traffic
	var totalRx, totalTx uint64
	for _, iface := range metrics.Network {
		totalRx += iface.RxBytes
		totalTx += iface.TxBytes
	}

	status := &ModemStatus{
		Timestamp:   time.Now().Unix(),
		Online:      false,
		SystemState: "normal",
		DeviceModel: p.identity.Model,
		Revision:    p.identity.Revision,
		Serial:      p.identity.Serial,
		Device: DeviceObject{
			Model:         p.identity.Model,
			Firmware:      p.identity.Revision,
			Manufacturer:  "Quectel",
			CPUUsage:      metrics.MemUsagePct, // approximation
			MemoryTotalMB: totalMB,
			MemoryUsedMB:  usedMB,
			UptimeSeconds: metrics.UptimeSeconds,
			Temperature:   &metrics.CpuTempC,
		},
		System: SystemObject{
			CPUUsagePct:   0.0,
			CPUUsage:      0.0,
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
			Status:   "ready",
			Inserted: true,
			Slot:     1,
		},
		Connectivity: ConnectivityObject{
			Status: "connected",
		},
		SignalPerAntenna: SignalPerAntenna{
			RSRP: []int{},
			RSRQ: []int{},
			SINR: []int{},
		},
		LTE: LteStatusObject{
			State: "disconnected",
		},
		NR: NrStatusObject{
			State: "disconnected",
		},
		Network: NetworkObject{
			Type:              "LTE",
			ServiceStatus:     "no_service",
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

			// Populate nested cell
			cidInt, _ := strconv.Atoi(cell.CellID)
			status.Cell = CellObject{
				CellID:    cell.CellID,
				PCID:      cell.PCID,
				EARFCN:    cell.EARFCN,
				Band:      cell.Band,
				Bandwidth: cell.Bandwidth,
				ENodeBID:  cidInt >> 8,
				SectorID:  cidInt & 0xFF,
			}

			// Populate LTE status object
			pciVal := cell.PCID
			earfcnVal := cell.EARFCN
			cellIdVal := cidInt
			enodebVal := cidInt >> 8
			sectorVal := cidInt & 0xFF
			rsrpVal := cell.RSRP
			rsrqVal := cell.RSRQ
			sinrVal := cell.SINR
			rssiVal := cell.RSSI

			status.LTE = LteStatusObject{
				State:    cell.State,
				Band:     cell.Band,
				EARFCN:   &earfcnVal,
				PCI:      &pciVal,
				CellID:   &cellIdVal,
				ENodeBID: &enodebVal,
				SectorID: &sectorVal,
				RSRP:     &rsrpVal,
				RSRQ:     &rsrqVal,
				SINR:     &sinrVal,
				RSSI:     &rssiVal,
			}

			// Populate Network object
			status.Network.Type = cell.Mode
			status.Network.Tech = cell.Mode
			status.Network.MCC = cell.MCC
			status.Network.MNC = cell.MNC
			status.Network.Registered = status.Online
			if status.Online {
				status.Network.ServiceStatus = "excellent"
			}
		}
	}

	// Micro-sleep between consecutive low-priority AT commands in 1Hz cycle
	time.Sleep(15 * time.Millisecond)

	// 2. Carrier aggregation
	if res, err := p.engine.ExecLow(ctx, `AT+QCAINFO`); err == nil {
		caList := atengine.ParseQCAINFO(res.Raw)
		status.CA = caList
		status.Network.CarrierComponents = caList
		status.Network.CACount = len(caList)
		status.Network.CAActive = len(caList) > 1

		totalBW := 0
		var bwDetails []string
		for _, comp := range caList {
			totalBW += comp.BandwidthMHz
			if comp.Band != "" && comp.BandwidthMHz > 0 {
				bwDetails = append(bwDetails, comp.Band+": "+strconv.Itoa(comp.BandwidthMHz)+" MHz")
			}
		}
		status.Network.TotalBandwidthMHz = totalBW
		status.Network.BandwidthDetails = strings.Join(bwDetails, " + ")
	}

	// Micro-sleep before CSQ query
	time.Sleep(15 * time.Millisecond)

	// 3. Signal CSQ fallback if RSRP not parsed
	if status.RSSI == 0 {
		if res, err := p.engine.ExecLow(ctx, `AT+CSQ`); err == nil {
			if csq := atengine.ParseCSQ(res.Raw); csq != nil {
				status.RSSI = csq.RSRPDbm
				status.Signal.RSSI = csq.RSRPDbm
			}
		}
	}

	p.mu.Lock()
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
