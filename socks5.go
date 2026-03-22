package main

import (
	"crypto/subtle"
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

// socks5ReadGreeting reads SOCKS5 greeting. If user/pass are non-empty, requires
// username/password auth per RFC 1929; otherwise accepts no-auth connections.
func socks5ReadGreeting(r io.Reader, w io.Writer, user, pass string) error {
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

	if user == "" {
		_, err := w.Write([]byte{0x05, 0x00})
		return err
	}

	// Require method 0x02 (username/password, RFC 1929).
	hasPassAuth := false
	for _, m := range methods {
		if m == 0x02 {
			hasPassAuth = true
			break
		}
	}
	if !hasPassAuth {
		_, _ = w.Write([]byte{0x05, 0xFF}) // no acceptable methods
		return fmt.Errorf("client does not support password auth")
	}
	if _, err := w.Write([]byte{0x05, 0x02}); err != nil {
		return err
	}

	// Auth sub-negotiation (RFC 1929).
	var ver [1]byte
	if _, err := io.ReadFull(r, ver[:]); err != nil {
		return err
	}
	if ver[0] != 1 {
		return fmt.Errorf("bad auth subnegotiation version %d", ver[0])
	}
	var ulen [1]byte
	if _, err := io.ReadFull(r, ulen[:]); err != nil {
		return err
	}
	uname := make([]byte, int(ulen[0]))
	if _, err := io.ReadFull(r, uname); err != nil {
		return err
	}
	var plen [1]byte
	if _, err := io.ReadFull(r, plen[:]); err != nil {
		return err
	}
	passwd := make([]byte, int(plen[0]))
	if _, err := io.ReadFull(r, passwd); err != nil {
		return err
	}

	userMatch := subtle.ConstantTimeCompare(uname, []byte(user))
	passMatch := subtle.ConstantTimeCompare(passwd, []byte(pass))
	if (userMatch & passMatch) != 1 {
		_, _ = w.Write([]byte{0x01, 0xFF})
		return fmt.Errorf("auth failed")
	}
	_, err := w.Write([]byte{0x01, 0x00})
	return err
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
