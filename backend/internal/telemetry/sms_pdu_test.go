package telemetry

import (
	"testing"
)

func TestDecodeSemiOctet(t *testing.T) {
	// "268801207100" -> "628810021700"
	got := DecodeSemiOctet("268801207100")
	if got != "628810021700" {
		t.Fatalf("expected 628810021700, got %s", got)
	}

	// Odd length with trailing F
	got2 := DecodeSemiOctet("12345F")
	if got2 != "21435" {
		t.Fatalf("expected 21435, got %s", got2)
	}
}

func TestGSM7ToUTF8(t *testing.T) {
	src := []byte{'H', 'e', 'l', 'l', 'o'}
	got := GSM7ToUTF8(src)
	if got != "Hello" {
		t.Fatalf("expected Hello, got %s", got)
	}
}

func TestDecodePDU_SingleAndMultipart(t *testing.T) {
	// Single SMS PDU
	pduSingle := "07912688012071002410D0D366504A354A8B4E0001629050619405828BD3323BDC0ED359A065B85D0791C3F0301D247C3AAB53D0BAFEA68741B5A3F03583A1E5219033BD6E87E96990F8EDAECFDDF930881C7683E865797D0E8287D7E134481D96A7DDE7B01B346D06A554A3B4E80C8186E53548FC76D7E720721A3477A5C92FF89A1E1ED35DA0A4DBFCD68170389C0B940D0661B0180D"
	item, err := DecodePDU(pduSingle, 0, "ME")
	if err != nil {
		t.Fatalf("DecodePDU failed: %v", err)
	}
	if item.Sender != "SMARTFREN" {
		t.Fatalf("expected sender SMARTFREN, got %s", item.Sender)
	}
	if item.IsUDH {
		t.Fatalf("expected single message to not be UDH")
	}

	// Multipart part 1
	pduPart1 := "07912688012071006410D0D366504A354A8B4E0001629050619484828D050003560201A66576B81DA6B340CB70BB0E2287E1613A48F87456A7A075FD4D0F836AB0A3F03583A1E5A035BA3EAFCF41E434481D96A7DDE7B01B543B8640C1353D6D5E87DD2075589E769FC36E50ED089AAFE5671039EC06B9D3EB76989E0689DFEEFADC9D0F8740C3F21A247EBBEB7310390D9ABBD2E4177C4D07"
	item1, err := DecodePDU(pduPart1, 0, "ME")
	if err != nil {
		t.Fatalf("DecodePDU part 1 failed: %v", err)
	}
	if !item1.IsUDH || item1.Part != 1 || item1.Total != 2 || item1.Ref != 86 {
		t.Fatalf("expected UDH ref 86 part 1/2, got ref %d part %d/%d isUDH=%v", item1.Ref, item1.Part, item1.Total, item1.IsUDH)
	}

	// Multipart part 2
	pduPart2 := "07912688012071004410D0D366504A354A8B4E0001629050619494821E050003560202C263BA0B94749BDF3A100E877381B2C1200C16AB01"
	item2, err := DecodePDU(pduPart2, 1, "ME")
	if err != nil {
		t.Fatalf("DecodePDU part 2 failed: %v", err)
	}
	if !item2.IsUDH || item2.Part != 2 || item2.Total != 2 || item2.Ref != 86 {
		t.Fatalf("expected UDH ref 86 part 2/2, got ref %d part %d/%d isUDH=%v", item2.Ref, item2.Part, item2.Total, item2.IsUDH)
	}

	// Reassembly
	reassembled := ReassembleMultipartSMS([]PDUItem{*item1, *item2})
	if len(reassembled) != 1 {
		t.Fatalf("expected 1 reassembled message, got %d", len(reassembled))
	}
	if len(reassembled[0].Indexes) != 2 || reassembled[0].Indexes[0] != 0 || reassembled[0].Indexes[1] != 1 {
		t.Fatalf("expected indexes [0, 1], got %v", reassembled[0].Indexes)
	}
	if len(reassembled[0].Content) < 100 {
		t.Fatalf("expected concatenated long content, got: %s", reassembled[0].Content)
	}
}
