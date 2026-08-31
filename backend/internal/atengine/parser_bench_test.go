package atengine

import (
	"testing"
)

var (
	benchRawQENG_LTE     = `+QENG: "servingcell","NOCONN","LTE","FDD",510,11,1A2B3C,218,1675,3,5,5,9A4F,-85,-9,-62,18,0,-`
	benchRawQENG_NR5G_NSA = `+QENG: "servingcell","NOCONN","NR5G-NSA",510,11,320,-78,25,-10,627392,78,100`
	benchRawQENG_NR5G_SA  = `+QENG: "servingcell","NOCONN","NR5G-SA","TDD",510,11,402100,320,1234,627392,78,100,-78,-10,25`
	benchRawQCAINFO       = "+QCAINFO: \"PCC\",1675,100,\"LTE BAND 3\",1,218,-85,-9,-62,18\r\n+QCAINFO: \"SCC\",300,50,\"LTE BAND 1\",1,120,-90,-11,-68,14\r\nOK"
	benchRawCSQ           = "+CSQ: 24,99\r\nOK"
	benchRawURC           = "+QIURC: \"recv\",0,4\r\n+CSQ: 24,99\r\nOK\r\n"
)

var sinkCellInfo *CellInfo
var sinkCA []CarrierComponent
var sinkCSQ *SignalQuality
var sinkString string

func BenchmarkParseQENGServingCell_LTE(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkCellInfo = ParseQENGServingCell(benchRawQENG_LTE)
	}
}

func BenchmarkParseQENGServingCell_NR5G_NSA(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkCellInfo = ParseQENGServingCell(benchRawQENG_NR5G_NSA)
	}
}

func BenchmarkParseQENGServingCell_NR5G_SA(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkCellInfo = ParseQENGServingCell(benchRawQENG_NR5G_SA)
	}
}

func BenchmarkParseQCAINFO(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkCA = ParseQCAINFO(benchRawQCAINFO)
	}
}

func BenchmarkParseCSQ(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkCSQ = ParseCSQ(benchRawCSQ)
	}
}

func BenchmarkFilterURC(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkString = FilterURC(benchRawURC)
	}
}
