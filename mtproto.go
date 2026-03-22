package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
)

var validProtos = map[uint32]struct{}{
	0xEFEFEFEF: {},
	0xEEEEEEEE: {},
	0xDDDDDDDD: {},
}

// DcFromInit extracts DC id and media flag from the 64-byte MTProto obfuscation init packet.
func DcFromInit(data []byte) (dc int, isMedia bool, ok bool) {
	if len(data) < 64 {
		return 0, false, false
	}
	block, err := aes.NewCipher(data[8:40])
	if err != nil {
		return 0, false, false
	}
	stream := cipher.NewCTR(block, data[40:56])
	var ks [64]byte
	stream.XORKeyStream(ks[:], ks[:])

	plain8 := binary.BigEndian.Uint64(data[56:64]) ^ binary.BigEndian.Uint64(ks[56:64])
	var plain [8]byte
	binary.BigEndian.PutUint64(plain[:], plain8)

	proto := binary.LittleEndian.Uint32(plain[:4])
	dcRaw := int16(binary.LittleEndian.Uint16(plain[4:6])) // #nosec G115 -- intentional: MTProto encodes DC id as signed int16 (negative = media DC)
	if _, vp := validProtos[proto]; !vp {
		return 0, false, false
	}
	ad := int(dcRaw)
	if ad < 0 {
		ad = -ad
	}
	if (ad >= 1 && ad <= 5) || ad == 203 {
		return ad, dcRaw < 0, true
	}
	return 0, false, false
}

// PatchInitDC patches dc_id bytes in the 64-byte MTProto init packet (Android useSecret=0).
func PatchInitDC(data []byte, dcSigned int16) []byte {
	if len(data) < 64 {
		return data
	}
	block, err := aes.NewCipher(data[8:40])
	if err != nil {
		return data
	}
	stream := cipher.NewCTR(block, data[40:56])
	var ks [64]byte
	stream.XORKeyStream(ks[:], ks[:])

	var newDC [2]byte
	binary.LittleEndian.PutUint16(newDC[:], uint16(dcSigned)) // #nosec G115 -- intentional: round-trip re-encoding of signed DC id into wire format

	out := make([]byte, len(data))
	copy(out, data)
	out[60] = ks[60] ^ newDC[0]
	out[61] = ks[61] ^ newDC[1]
	return out
}

// IsHTTPTransport detects HTTP-like first bytes (MTProto over HTTP).
func IsHTTPTransport(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	if len(data) >= 5 && string(data[:5]) == "POST " {
		return true
	}
	if string(data[:4]) == "GET " {
		return true
	}
	if len(data) >= 5 && string(data[:5]) == "HEAD " {
		return true
	}
	if len(data) >= 8 && string(data[:8]) == "OPTIONS " {
		return true
	}
	return false
}

// MsgSplitter splits batched MTProto abridged messages for per-WS-frame send.
type MsgSplitter struct {
	stream cipher.Stream
}

// NewMsgSplitter builds a splitter from the 64-byte init; skips the init block in CTR stream.
func NewMsgSplitter(initData []byte) (*MsgSplitter, error) {
	if len(initData) < 64 {
		return nil, errors.New("init too short")
	}
	block, err := aes.NewCipher(initData[8:40])
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, initData[40:56])
	var skip [64]byte
	stream.XORKeyStream(skip[:], skip[:])
	return &MsgSplitter{stream: stream}, nil
}

// Split returns ciphertext chunks aligned to MTProto message boundaries.
func (m *MsgSplitter) Split(chunk []byte) [][]byte {
	if m == nil || len(chunk) == 0 {
		return [][]byte{chunk}
	}
	plain := make([]byte, len(chunk))
	copy(plain, chunk)
	m.stream.XORKeyStream(plain, plain)

	var boundaries []int
	pos := 0
	for pos < len(plain) {
		first := plain[pos]
		var msgLen int
		if first == 0x7f {
			if pos+4 > len(plain) {
				break
			}
			msgLen = (int(plain[pos+1]) | int(plain[pos+2])<<8 | int(plain[pos+3])<<16) * 4
			pos += 4
		} else {
			msgLen = int(first) * 4
			pos++
		}
		if msgLen == 0 || pos+msgLen > len(plain) {
			break
		}
		pos += msgLen
		boundaries = append(boundaries, pos)
	}
	if len(boundaries) <= 1 {
		return [][]byte{chunk}
	}
	parts := make([][]byte, 0, len(boundaries)+1)
	prev := 0
	for _, b := range boundaries {
		parts = append(parts, chunk[prev:b])
		prev = b
	}
	if prev < len(chunk) {
		parts = append(parts, chunk[prev:])
	}
	return parts
}
