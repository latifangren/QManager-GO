package handlers

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// FrequencyCalculatorHandler handles calculation requests.
type FrequencyCalculatorHandler struct{}

// NewFrequencyCalculatorHandler creates a new FrequencyCalculatorHandler.
func NewFrequencyCalculatorHandler() *FrequencyCalculatorHandler {
	return &FrequencyCalculatorHandler{}
}

// Calculate handles GET /api/v1/cellular/frequency/calculate?tech=LTE&channel=1675
func (h *FrequencyCalculatorHandler) Calculate(w http.ResponseWriter, r *http.Request) {
	tech := strings.ToUpper(r.URL.Query().Get("tech"))
	chanStr := r.URL.Query().Get("channel")
	if chanStr == "" {
		chanStr = r.URL.Query().Get("arfcn")
	}
	if chanStr == "" {
		chanStr = r.URL.Query().Get("earfcn")
	}

	channel, err := strconv.Atoi(chanStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid channel/ARFCN number")
		return
	}

	var res FrequencyCalcResult
	if tech == "NR" || tech == "NR5G" || tech == "5G" {
		res = CalculateNRFrequency(channel)
	} else {
		res = CalculateLTEFrequency(channel)
	}

	JSON(w, http.StatusOK, res)
}

// EARFCN / NR-ARFCN to Frequency & Band calculations
// Based on 3GPP TS 36.101 (LTE) and 3GPP TS 38.104 (NR).

// LTEBand defines an LTE band specification.
type LTEBand struct {
	Band         int
	Name         string
	DLLow        float64
	ULLow        float64
	EARFCNOffset int
	ULEARFCNOffset int
	Duplex       string // "FDD" or "TDD"
	EARFCNRange  [2]int
	ULRange      [2]int
}

// NRBand defines an NR band specification.
type NRBand struct {
	Band        int
	Name        string
	DLLow       float64
	DLHigh      float64
	ULLow       float64
	ULHigh      float64
	Duplex      string // "FDD", "TDD", or "SDL"
	NRARFCNRange [2]int
}

// FrequencyCalcResult is returned when evaluating an ARFCN/EARFCN.
type FrequencyCalcResult struct {
	Valid          bool            `json:"valid"`
	Technology     string          `json:"technology"`
	InputARFCN     int             `json:"input_arfcn"`
	DLFrequencyMHz float64         `json:"dl_frequency_mhz"`
	ULFrequencyMHz *float64        `json:"ul_frequency_mhz,omitempty"`
	MatchingBands  []BandMatchInfo `json:"matching_bands"`
}

// BandMatchInfo contains details for a single matching band.
type BandMatchInfo struct {
	Band           string   `json:"band"`
	Name           string   `json:"name"`
	Duplex         string   `json:"duplex"`
	DLFrequencyMHz float64  `json:"dl_frequency_mhz"`
	ULFrequencyMHz *float64 `json:"ul_frequency_mhz,omitempty"`
	ULChannel      *int     `json:"ul_channel,omitempty"`
}

var LTEBands = []LTEBand{
	{Band: 1, Name: "2100", DLLow: 2110, ULLow: 1920, EARFCNOffset: 0, ULEARFCNOffset: 18000, Duplex: "FDD", EARFCNRange: [2]int{0, 599}, ULRange: [2]int{18000, 18599}},
	{Band: 2, Name: "1900 PCS", DLLow: 1930, ULLow: 1850, EARFCNOffset: 600, ULEARFCNOffset: 18600, Duplex: "FDD", EARFCNRange: [2]int{600, 1199}, ULRange: [2]int{18600, 19199}},
	{Band: 3, Name: "1800+", DLLow: 1805, ULLow: 1710, EARFCNOffset: 1200, ULEARFCNOffset: 19200, Duplex: "FDD", EARFCNRange: [2]int{1200, 1949}, ULRange: [2]int{19200, 19949}},
	{Band: 4, Name: "AWS-1", DLLow: 2110, ULLow: 1710, EARFCNOffset: 1950, ULEARFCNOffset: 19950, Duplex: "FDD", EARFCNRange: [2]int{1950, 2399}, ULRange: [2]int{19950, 20399}},
	{Band: 5, Name: "850", DLLow: 869, ULLow: 824, EARFCNOffset: 2400, ULEARFCNOffset: 20400, Duplex: "FDD", EARFCNRange: [2]int{2400, 2649}, ULRange: [2]int{20400, 20649}},
	{Band: 7, Name: "2600", DLLow: 2620, ULLow: 2500, EARFCNOffset: 2750, ULEARFCNOffset: 20750, Duplex: "FDD", EARFCNRange: [2]int{2750, 3449}, ULRange: [2]int{20750, 21449}},
	{Band: 8, Name: "900", DLLow: 925, ULLow: 880, EARFCNOffset: 3450, ULEARFCNOffset: 21450, Duplex: "FDD", EARFCNRange: [2]int{3450, 3799}, ULRange: [2]int{21450, 21799}},
	{Band: 12, Name: "700 a/b/c", DLLow: 729, ULLow: 699, EARFCNOffset: 5010, ULEARFCNOffset: 23010, Duplex: "FDD", EARFCNRange: [2]int{5010, 5179}, ULRange: [2]int{23010, 23179}},
	{Band: 13, Name: "700 c", DLLow: 746, ULLow: 777, EARFCNOffset: 5180, ULEARFCNOffset: 23180, Duplex: "FDD", EARFCNRange: [2]int{5180, 5279}, ULRange: [2]int{23180, 23279}},
	{Band: 14, Name: "700 PS", DLLow: 758, ULLow: 788, EARFCNOffset: 5280, ULEARFCNOffset: 23280, Duplex: "FDD", EARFCNRange: [2]int{5280, 5379}, ULRange: [2]int{23280, 23379}},
	{Band: 17, Name: "700 b/c", DLLow: 734, ULLow: 704, EARFCNOffset: 5730, ULEARFCNOffset: 23730, Duplex: "FDD", EARFCNRange: [2]int{5730, 5849}, ULRange: [2]int{23730, 23849}},
	{Band: 18, Name: "800 Lower", DLLow: 860, ULLow: 815, EARFCNOffset: 5850, ULEARFCNOffset: 23850, Duplex: "FDD", EARFCNRange: [2]int{5850, 5999}, ULRange: [2]int{23850, 23999}},
	{Band: 19, Name: "800 Upper", DLLow: 875, ULLow: 830, EARFCNOffset: 6000, ULEARFCNOffset: 24000, Duplex: "FDD", EARFCNRange: [2]int{6000, 6149}, ULRange: [2]int{24000, 24149}},
	{Band: 20, Name: "800 DD", DLLow: 791, ULLow: 832, EARFCNOffset: 6150, ULEARFCNOffset: 24150, Duplex: "FDD", EARFCNRange: [2]int{6150, 6449}, ULRange: [2]int{24150, 24449}},
	{Band: 25, Name: "1900+", DLLow: 1930, ULLow: 1850, EARFCNOffset: 8040, ULEARFCNOffset: 26040, Duplex: "FDD", EARFCNRange: [2]int{8040, 8689}, ULRange: [2]int{26040, 26689}},
	{Band: 26, Name: "850+", DLLow: 859, ULLow: 814, EARFCNOffset: 8690, ULEARFCNOffset: 26690, Duplex: "FDD", EARFCNRange: [2]int{8690, 9039}, ULRange: [2]int{26690, 27039}},
	{Band: 28, Name: "700 APT", DLLow: 758, ULLow: 703, EARFCNOffset: 9210, ULEARFCNOffset: 27210, Duplex: "FDD", EARFCNRange: [2]int{9210, 9659}, ULRange: [2]int{27210, 27659}},
	{Band: 34, Name: "2000", DLLow: 2010, ULLow: 2010, EARFCNOffset: 36200, ULEARFCNOffset: 36200, Duplex: "TDD", EARFCNRange: [2]int{36200, 36349}, ULRange: [2]int{36200, 36349}},
	{Band: 38, Name: "2600", DLLow: 2570, ULLow: 2570, EARFCNOffset: 37750, ULEARFCNOffset: 37750, Duplex: "TDD", EARFCNRange: [2]int{37750, 38249}, ULRange: [2]int{37750, 38249}},
	{Band: 39, Name: "1900+", DLLow: 1880, ULLow: 1880, EARFCNOffset: 38250, ULEARFCNOffset: 38250, Duplex: "TDD", EARFCNRange: [2]int{38250, 38649}, ULRange: [2]int{38250, 38649}},
	{Band: 40, Name: "2300", DLLow: 2300, ULLow: 2300, EARFCNOffset: 38650, ULEARFCNOffset: 38650, Duplex: "TDD", EARFCNRange: [2]int{38650, 39649}, ULRange: [2]int{38650, 39649}},
	{Band: 41, Name: "2500 BRS/EBS", DLLow: 2496, ULLow: 2496, EARFCNOffset: 39650, ULEARFCNOffset: 39650, Duplex: "TDD", EARFCNRange: [2]int{39650, 41589}, ULRange: [2]int{39650, 41589}},
	{Band: 42, Name: "3500", DLLow: 3400, ULLow: 3400, EARFCNOffset: 41590, ULEARFCNOffset: 41590, Duplex: "TDD", EARFCNRange: [2]int{41590, 43589}, ULRange: [2]int{41590, 43589}},
	{Band: 43, Name: "3700", DLLow: 3600, ULLow: 3600, EARFCNOffset: 43590, ULEARFCNOffset: 43590, Duplex: "TDD", EARFCNRange: [2]int{43590, 45589}, ULRange: [2]int{43590, 45589}},
	{Band: 48, Name: "3600 CBRS", DLLow: 3550, ULLow: 3550, EARFCNOffset: 55240, ULEARFCNOffset: 55240, Duplex: "TDD", EARFCNRange: [2]int{55240, 56739}, ULRange: [2]int{55240, 56739}},
	{Band: 66, Name: "AWS-3", DLLow: 2110, ULLow: 1710, EARFCNOffset: 66436, ULEARFCNOffset: 131972, Duplex: "FDD", EARFCNRange: [2]int{66436, 67335}, ULRange: [2]int{131972, 132671}},
	{Band: 71, Name: "600", DLLow: 617, ULLow: 663, EARFCNOffset: 68586, ULEARFCNOffset: 133122, Duplex: "FDD", EARFCNRange: [2]int{68586, 68935}, ULRange: [2]int{133122, 133471}},
}

var NRBands = []NRBand{
	{Band: 1, Name: "2100", DLLow: 2110, DLHigh: 2170, ULLow: 1920, ULHigh: 1980, Duplex: "FDD", NRARFCNRange: [2]int{422000, 434000}},
	{Band: 2, Name: "1900 PCS", DLLow: 1930, DLHigh: 1990, ULLow: 1850, ULHigh: 1910, Duplex: "FDD", NRARFCNRange: [2]int{386000, 398000}},
	{Band: 3, Name: "1800", DLLow: 1805, DLHigh: 1880, ULLow: 1710, ULHigh: 1785, Duplex: "FDD", NRARFCNRange: [2]int{361000, 376000}},
	{Band: 5, Name: "850", DLLow: 869, DLHigh: 894, ULLow: 824, ULHigh: 849, Duplex: "FDD", NRARFCNRange: [2]int{173800, 178800}},
	{Band: 7, Name: "2600", DLLow: 2620, DLHigh: 2690, ULLow: 2500, ULHigh: 2570, Duplex: "FDD", NRARFCNRange: [2]int{524000, 538000}},
	{Band: 8, Name: "900", DLLow: 925, DLHigh: 960, ULLow: 880, ULHigh: 915, Duplex: "FDD", NRARFCNRange: [2]int{185000, 192000}},
	{Band: 12, Name: "700 a", DLLow: 729, DLHigh: 746, ULLow: 699, ULHigh: 716, Duplex: "FDD", NRARFCNRange: [2]int{145800, 149200}},
	{Band: 20, Name: "800 DD", DLLow: 791, DLHigh: 821, ULLow: 832, ULHigh: 862, Duplex: "FDD", NRARFCNRange: [2]int{158200, 164200}},
	{Band: 25, Name: "1900+", DLLow: 1930, DLHigh: 1995, ULLow: 1850, ULHigh: 1915, Duplex: "FDD", NRARFCNRange: [2]int{386000, 399000}},
	{Band: 28, Name: "700 APT", DLLow: 758, DLHigh: 803, ULLow: 703, ULHigh: 748, Duplex: "FDD", NRARFCNRange: [2]int{151600, 160600}},
	{Band: 38, Name: "2600", DLLow: 2570, DLHigh: 2620, ULLow: 2570, ULHigh: 2620, Duplex: "TDD", NRARFCNRange: [2]int{514000, 524000}},
	{Band: 40, Name: "2300", DLLow: 2300, DLHigh: 2400, ULLow: 2300, ULHigh: 2400, Duplex: "TDD", NRARFCNRange: [2]int{460000, 480000}},
	{Band: 41, Name: "2500 BRS/EBS", DLLow: 2496, DLHigh: 2690, ULLow: 2496, ULHigh: 2690, Duplex: "TDD", NRARFCNRange: [2]int{499200, 537999}},
	{Band: 48, Name: "3600 CBRS", DLLow: 3550, DLHigh: 3700, ULLow: 3550, ULHigh: 3700, Duplex: "TDD", NRARFCNRange: [2]int{636667, 646666}},
	{Band: 66, Name: "AWS-3", DLLow: 2110, DLHigh: 2200, ULLow: 1710, ULHigh: 1780, Duplex: "FDD", NRARFCNRange: [2]int{422000, 440000}},
	{Band: 71, Name: "600", DLLow: 617, DLHigh: 652, ULLow: 663, ULHigh: 698, Duplex: "FDD", NRARFCNRange: [2]int{123400, 130400}},
	{Band: 77, Name: "3700 C-Band", DLLow: 3300, DLHigh: 4200, ULLow: 3300, ULHigh: 4200, Duplex: "TDD", NRARFCNRange: [2]int{620000, 680000}},
	{Band: 78, Name: "3500", DLLow: 3300, DLHigh: 3800, ULLow: 3300, ULHigh: 3800, Duplex: "TDD", NRARFCNRange: [2]int{620000, 653333}},
	{Band: 79, Name: "4700", DLLow: 4400, DLHigh: 5000, ULLow: 4400, ULHigh: 5000, Duplex: "TDD", NRARFCNRange: [2]int{693334, 733333}},
}

// CalculateLTEFrequency calculates DL frequency and UL info for an EARFCN.
func CalculateLTEFrequency(earfcn int) FrequencyCalcResult {
	res := FrequencyCalcResult{
		Technology: "LTE",
		InputARFCN: earfcn,
	}

	var matches []BandMatchInfo
	for _, b := range LTEBands {
		if earfcn >= b.EARFCNRange[0] && earfcn <= b.EARFCNRange[1] {
			dlFreq := b.DLLow + 0.1*float64(earfcn-b.EARFCNOffset)
			dlFreq = math.Round(dlFreq*100) / 100

			var ulFreq *float64
			var ulChan *int

			if b.Duplex == "TDD" {
				ulFreq = &dlFreq
				ulChan = &earfcn
			} else {
				ulCalc := b.ULLow + 0.1*float64(earfcn-b.EARFCNOffset)
				ulCalc = math.Round(ulCalc*100) / 100
				ulFreq = &ulCalc

				ulC := earfcn - b.EARFCNOffset + b.ULEARFCNOffset
				ulChan = &ulC
			}

			res.DLFrequencyMHz = dlFreq
			if res.ULFrequencyMHz == nil && ulFreq != nil {
				res.ULFrequencyMHz = ulFreq
			}

			matches = append(matches, BandMatchInfo{
				Band:           fmt.Sprintf("B%d", b.Band),
				Name:           b.Name,
				Duplex:         b.Duplex,
				DLFrequencyMHz: dlFreq,
				ULFrequencyMHz: ulFreq,
				ULChannel:      ulChan,
			})
		}
	}

	if len(matches) > 0 {
		res.Valid = true
		res.MatchingBands = matches
	}

	return res
}

// CalculateNRFrequency calculates DL frequency and matching bands for an NR-ARFCN according to TS 38.104 global raster.
func CalculateNRFrequency(nrarfcn int) FrequencyCalcResult {
	res := FrequencyCalcResult{
		Technology: "NR5G",
		InputARFCN: nrarfcn,
	}

	dlFreq := nrArfcnToFrequency(nrarfcn)
	if dlFreq == 0 {
		return res
	}

	res.DLFrequencyMHz = dlFreq
	var matches []BandMatchInfo

	for _, b := range NRBands {
		if nrarfcn >= b.NRARFCNRange[0] && nrarfcn <= b.NRARFCNRange[1] {
			var ulFreq *float64
			if b.Duplex == "TDD" {
				ulFreq = &dlFreq
			} else if b.Duplex == "FDD" {
				// Offset calculated based on band
				freqOffset := b.DLLow - b.ULLow
				ul := dlFreq - freqOffset
				ul = math.Round(ul*100) / 100
				ulFreq = &ul
			}

			if res.ULFrequencyMHz == nil && ulFreq != nil {
				res.ULFrequencyMHz = ulFreq
			}

			matches = append(matches, BandMatchInfo{
				Band:           fmt.Sprintf("n%d", b.Band),
				Name:           b.Name,
				Duplex:         b.Duplex,
				DLFrequencyMHz: dlFreq,
				ULFrequencyMHz: ulFreq,
			})
		}
	}

	if len(matches) > 0 {
		res.Valid = true
		res.MatchingBands = matches
	}

	return res
}

// nrArfcnToFrequency converts global NR-ARFCN to DL frequency in MHz (TS 38.104 §5.4.2.1)
func nrArfcnToFrequency(nrarfcn int) float64 {
	if nrarfcn < 0 || nrarfcn > 3279165 {
		return 0
	}
	var f float64
	if nrarfcn <= 599999 {
		// 0 - 3000 MHz (ΔF_Global = 5 kHz = 0.005 MHz)
		f = 0.0 + 0.005*float64(nrarfcn)
	} else if nrarfcn <= 2016666 {
		// 3000 - 24250 MHz (ΔF_Global = 15 kHz = 0.015 MHz)
		f = 3000.0 + 0.015*float64(nrarfcn-600000)
	} else {
		// 24250 - 100000 MHz (ΔF_Global = 60 kHz = 0.060 MHz)
		f = 24250.08 + 0.060*float64(nrarfcn-2016667)
	}
	return math.Round(f*100) / 100
}
