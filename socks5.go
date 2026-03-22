package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

var socks5Replies = map[byte][]byte{
	0x00: {0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0},
	0x05: {0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0},
	0x07: {0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0},
	0x08: {0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0},
}

func socks5Reply(status byte) []byte {
	if b, ok := socks5Replies[status]; ok {
		return b
	}
	return socks5Replies[0x05]
}

// socks5ReadGreeting reads SOCKS5 greeting and writes no-auth response.
func socks5ReadGreeting(r io.Reader, w io.Writer) error {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != 5 {
		return fmt.Errorf("not SOCKS5 version %d", hdr[0])
	}
	nmethods := int(hdr[1])
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(r, methods); err != nil {
		return err
	}
	if _, err := w.Write([]byte{0x05, 0x00}); err != nil {
		return err
	}
	return nil
}

// socks5ReadConnect reads SOCKS5 CONNECT and returns IPv4/domain destination, port.
func socks5ReadConnect(r io.Reader) (dst string, port int, err error) {
	var req [4]byte
	if _, err := io.ReadFull(r, req[:]); err != nil {
		return "", 0, err
	}
	if req[0] != 5 {
		return "", 0, fmt.Errorf("bad SOCKS version")
	}
	if req[1] != 1 { // CONNECT
		return "", 0, errNotConnect
	}
	atyp := req[3]
	switch atyp {
	case 1: // IPv4
		var raw [4]byte
		if _, err := io.ReadFull(r, raw[:]); err != nil {
			return "", 0, err
		}
		dst = net.IP(raw[:]).String()
	case 3: // domain
		var dlen [1]byte
		if _, err := io.ReadFull(r, dlen[:]); err != nil {
			return "", 0, err
		}
		name := make([]byte, int(dlen[0]))
		if _, err := io.ReadFull(r, name); err != nil {
			return "", 0, err
		}
		dst = string(name)
	case 4: // IPv6
		var raw [16]byte
		if _, err := io.ReadFull(r, raw[:]); err != nil {
			return "", 0, err
		}
		dst = net.IP(raw[:]).String()
	default:
		return "", 0, errBadATYP
	}
	var pp [2]byte
	if _, err := io.ReadFull(r, pp[:]); err != nil {
		return "", 0, err
	}
	port = int(binary.BigEndian.Uint16(pp[:]))
	return dst, port, nil
}

var errNotConnect = fmt.Errorf("not CONNECT")
var errBadATYP = fmt.Errorf("bad ATYP")
