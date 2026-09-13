package telemetry

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// PDUItem holds decoded information from a single PDU message.
type PDUItem struct {
	Index     int
	Sender    string
	Timestamp string
	Text      string
	IsUDH     bool
	Ref       int
	Part      int
	Total     int
	Storage   string
}

// DecodeSemiOctet decodes swapped BCD string (e.g., "268801207100" -> "628810021700").
func DecodeSemiOctet(bcd string) string {
	var res strings.Builder
	for i := 0; i < len(bcd); i += 2 {
		if i+1 < len(bcd) {
			b2 := bcd[i+1]
			b1 := bcd[i]
			if b2 != 'F' && b2 != 'f' {
				res.WriteByte(b2)
			}
			if b1 != 'F' && b1 != 'f' {
				res.WriteByte(b1)
			}
		} else {
			res.WriteByte(bcd[i])
		}
	}
	return res.String()
}

// GSM7DefaultTable maps 7-bit standard GSM alphabet characters to UTF-8.
var gsm7DefaultTable = []rune{
	'@', '£', '$', '¥', 'è', 'é', 'ù', 'ì', 'ò', 'Ç', '\n', 'Ø', 'ø', '\r', 'Å', 'å',
	'Δ', '_', 'Φ', 'Γ', 'Λ', 'Ω', 'Π', 'Ψ', 'Σ', 'Θ', 'Ξ', '\x1b', 'Æ', 'æ', 'ß', 'É',
	' ', '!', '"', '#', '¤', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
	'¡', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
	'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'Ä', 'Ö', 'Ñ', 'Ü', '§',
	'¿', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 'ä', 'ö', 'ñ', 'ü', 'à',
}

// GSM7ToUTF8 converts 7-bit GSM bytes to standard Go string.
func GSM7ToUTF8(src []byte) string {
	var sb strings.Builder
	for _, b := range src {
		if int(b) < len(gsm7DefaultTable) {
			sb.WriteRune(gsm7DefaultTable[b])
		} else {
			sb.WriteByte(b)
		}
	}
	return sb.String()
}

// Unpack7Bit decodes packed 7-bit GSM septets into unpacked 7-bit bytes.
func Unpack7Bit(src []byte, septetCount int, offsetBits int) []byte {
	out := make([]byte, 0, septetCount)
	bitBuf := 0
	bitsInBuf := 0

	for _, b := range src {
		bitBuf |= int(b) << bitsInBuf
		bitsInBuf += 8
		for bitsInBuf >= 7+offsetBits {
			if offsetBits > 0 {
				bitBuf >>= offsetBits
				bitsInBuf -= offsetBits
				offsetBits = 0
			}
			if len(out) < septetCount {
				out = append(out, byte(bitBuf&0x7F))
			}
			bitBuf >>= 7
			bitsInBuf -= 7
		}
	}
	if bitsInBuf >= 7 && len(out) < septetCount {
		out = append(out, byte(bitBuf&0x7F))
	}
	return out
}

// DecodePDU parses raw hex PDU string into a PDUItem.
func DecodePDU(pduHex string, index int, storage string) (*PDUItem, error) {
	pduHex = strings.TrimSpace(pduHex)
	b, err := hex.DecodeString(pduHex)
	if err != nil || len(b) < 10 {
		return nil, fmt.Errorf("invalid PDU hex: %w", err)
	}

	// 1. SMSC Info
	smscLen := int(b[0])
	pos := 1 + smscLen
	if pos >= len(b) {
		return nil, fmt.Errorf("PDU too short after SMSC")
	}

	// 2. First octet
	firstOctet := b[pos]
	hasUDH := (firstOctet & 0x40) != 0
	pos++
	if pos >= len(b) {
		return nil, fmt.Errorf("PDU too short after first octet")
	}

	// 3. Sender Address
	senderLen := int(b[pos])
	pos++
	if pos >= len(b) {
		return nil, fmt.Errorf("PDU too short after sender length")
	}
	senderType := b[pos]
	pos++

	senderBytesLen := (senderLen + 1) / 2
	if pos+senderBytesLen > len(b) {
		return nil, fmt.Errorf("PDU sender address length out of bounds")
	}

	senderHex := pduHex[pos*2 : (pos+senderBytesLen)*2]
	pos += senderBytesLen

	var sender string
	if senderType == 0xD0 || senderType == 0xD1 {
		// Alphanumeric 7-bit address
		sBytes, _ := hex.DecodeString(senderHex)
		septets := (senderLen * 4) / 7
		unpacked := Unpack7Bit(sBytes, septets, 0)
		sender = GSM7ToUTF8(unpacked)
	} else {
		sender = DecodeSemiOctet(senderHex)
		if senderType == 0x91 && !strings.HasPrefix(sender, "+") {
			sender = "+" + sender
		}
	}

	if pos+9 > len(b) {
		return nil, fmt.Errorf("PDU missing TP-PID, TP-DCS, or SCTS")
	}

	// 4. Protocol ID & Data Coding Scheme (DCS)
	_ = b[pos] // tpPID
	pos++
	tpDCS := b[pos]
	pos++

	// 5. Service Centre Time Stamp (7 octets)
	sctsBytes := pduHex[pos*2 : (pos+7)*2]
	pos += 7
	sctsDecoded := DecodeSemiOctet(sctsBytes)
	timestamp := ""
	if len(sctsDecoded) >= 12 {
		timestamp = fmt.Sprintf("%s/%s/%s %s:%s:%s",
			sctsDecoded[2:4], sctsDecoded[4:6], sctsDecoded[0:2],
			sctsDecoded[6:8], sctsDecoded[8:10], sctsDecoded[10:12])
	}

	if pos >= len(b) {
		return nil, fmt.Errorf("PDU missing User Data Length")
	}

	// 6. User Data
	userDataLen := int(b[pos])
	pos++

	var udhRef, udhPart, udhTotal int
	userDataBytes := b[pos:]
	var text string

	// Check DCS: 0x08 (UCS2 / UTF-16BE), 0x04 (8-bit binary), default (7-bit GSM)
	isUCS2 := (tpDCS & 0x0C) == 0x08
	is8Bit := (tpDCS & 0x0C) == 0x04

	if hasUDH && len(userDataBytes) > 1 {
		udhLen := int(userDataBytes[0])
		if 1+udhLen <= len(userDataBytes) {
			udhData := userDataBytes[1 : 1+udhLen]
			for i := 0; i+2 < len(udhData); {
				iei := udhData[i]
				ieLen := int(udhData[i+1])
				if i+2+ieLen > len(udhData) {
					break
				}
				if (iei == 0x00 && ieLen == 3) || (iei == 0x08 && ieLen == 4) {
					if iei == 0x00 {
						udhRef = int(udhData[i+2])
						udhTotal = int(udhData[i+3])
						udhPart = int(udhData[i+4])
					} else {
						udhRef = int(udhData[i+2])<<8 | int(udhData[i+3])
						udhTotal = int(udhData[i+4])
						udhPart = int(udhData[i+5])
					}
				}
				i += 2 + ieLen
			}

			payloadBytes := userDataBytes[1+udhLen:]
			if isUCS2 {
				u16 := make([]uint16, len(payloadBytes)/2)
				for j := 0; j < len(u16); j++ {
					u16[j] = binary.BigEndian.Uint16(payloadBytes[j*2 : j*2+2])
				}
				text = string(utf16.Decode(u16))
			} else if is8Bit {
				text = string(payloadBytes)
			} else {
				// 7-bit GSM with UDH padding
				fillBits := (7 - ((1+udhLen)*8)%7) % 7
				septetCount := userDataLen - ((1+udhLen)*8+fillBits)/7
				unpacked := Unpack7Bit(payloadBytes, septetCount, fillBits)
				text = GSM7ToUTF8(unpacked)
			}
		}
	} else {
		if isUCS2 {
			u16 := make([]uint16, len(userDataBytes)/2)
			for j := 0; j < len(u16); j++ {
				u16[j] = binary.BigEndian.Uint16(userDataBytes[j*2 : j*2+2])
			}
			text = string(utf16.Decode(u16))
		} else if is8Bit {
			text = string(userDataBytes)
		} else {
			unpacked := Unpack7Bit(userDataBytes, userDataLen, 0)
			text = GSM7ToUTF8(unpacked)
		}
	}

	return &PDUItem{
		Index:     index,
		Sender:    sender,
		Timestamp: timestamp,
		Text:      text,
		IsUDH:     hasUDH && udhTotal > 1,
		Ref:       udhRef,
		Part:      udhPart,
		Total:     udhTotal,
		Storage:   storage,
	}, nil
}

// ParseCMGLPDU parses raw AT+CMGL output in PDU mode (AT+CMGF=0).
func ParseCMGLPDU(raw string, storage string) []PDUItem {
	var items []PDUItem
	lines := strings.Split(raw, "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "+CMGL:") {
			continue
		}

		parts := strings.Split(strings.TrimPrefix(line, "+CMGL:"), ",")
		if len(parts) < 1 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}

		// Next line is the hex PDU string
		if i+1 < len(lines) {
			pduHex := strings.TrimSpace(lines[i+1])
			i++
			if item, err := DecodePDU(pduHex, idx, storage); err == nil && item != nil {
				items = append(items, *item)
			}
		}
	}

	return items
}

// ReassembleMultipartSMS groups and reassembles multipart/concatenated SMS messages into unified SMSMessage objects.
func ReassembleMultipartSMS(pduItems []PDUItem) []SMSMessage {
	type multipartKey struct {
		Sender string
		Ref    int
		Total  int
	}

	multipartGroups := make(map[multipartKey][]PDUItem)
	var singleMessages []SMSMessage

	for _, item := range pduItems {
		if item.IsUDH && item.Total > 1 {
			key := multipartKey{
				Sender: item.Sender,
				Ref:    item.Ref,
				Total:  item.Total,
			}
			multipartGroups[key] = append(multipartGroups[key], item)
		} else {
			singleMessages = append(singleMessages, SMSMessage{
				Indexes:   []int{item.Index},
				Sender:    item.Sender,
				Content:   item.Text,
				Timestamp: item.Timestamp,
				Storage:   item.Storage,
			})
		}
	}

	for _, group := range multipartGroups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].Part < group[j].Part
		})

		var indexes []int
		var contentBuilder strings.Builder
		sender := ""
		timestamp := ""
		storage := "ME"

		for _, part := range group {
			indexes = append(indexes, part.Index)
			contentBuilder.WriteString(part.Text)
			if sender == "" {
				sender = part.Sender
			}
			if timestamp == "" {
				timestamp = part.Timestamp
			}
			storage = part.Storage
		}

		singleMessages = append(singleMessages, SMSMessage{
			Indexes:   indexes,
			Sender:    sender,
			Content:   contentBuilder.String(),
			Timestamp: timestamp,
			Storage:   storage,
		})
	}

	SortSMSMessages(singleMessages)
	return singleMessages
}
