package main

import (
	"encoding/binary"
	"net/netip"
	"strconv"
)

// Telegram DC IPv4 ranges (inclusive).
var tgRanges = []struct{ lo, hi uint32 }{
	mustIPv4Range("185.76.151.0", "185.76.151.255"),
	mustIPv4Range("149.154.160.0", "149.154.175.255"),
	mustIPv4Range("91.105.192.0", "91.105.193.255"),
	mustIPv4Range("91.108.0.0", "91.108.255.255"),
}

func mustIPv4Range(a, b string) struct{ lo, hi uint32 } {
	return struct{ lo, hi uint32 }{parseIPv4(a), parseIPv4(b)}
}

func parseIPv4(s string) uint32 {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		panic("tgips: bad IP " + s)
	}
	if !addr.Is4() {
		panic("tgips: not IPv4 " + s)
	}
	b := addr.As4()
	return binary.BigEndian.Uint32(b[:])
}

// IsTelegramIP reports whether s is an IPv4 address in Telegram DC ranges.
func IsTelegramIP(s string) bool {
	addr, err := netip.ParseAddr(s)
	if err != nil || !addr.Is4() {
		return false
	}
	a4 := addr.As4()
	n := binary.BigEndian.Uint32(a4[:])
	for _, r := range tgRanges {
		if n >= r.lo && n <= r.hi {
			return true
		}
	}
	return false
}

// DCEntry is IP -> (dc_id, is_media) from Python _IP_TO_DC.
type DCEntry struct {
	DC      int
	IsMedia bool
}

// ipToDC maps known DC host IPs to DC id and media flag.
var ipToDC = map[string]DCEntry{
	"149.154.175.50": {1, false}, "149.154.175.51": {1, false},
	"149.154.175.53": {1, false}, "149.154.175.54": {1, false},
	"149.154.175.52": {1, true},
	"149.154.167.41": {2, false}, "149.154.167.50": {2, false},
	"149.154.167.51": {2, false}, "149.154.167.220": {2, false},
	"95.161.76.100":   {2, false},
	"149.154.167.151": {2, true}, "149.154.167.222": {2, true},
	"149.154.167.223": {2, true}, "149.154.162.123": {2, true},
	"149.154.175.100": {3, false}, "149.154.175.101": {3, false},
	"149.154.175.102": {3, true},
	"149.154.167.91":  {4, false}, "149.154.167.92": {4, false},
	"149.154.164.250": {4, true}, "149.154.166.120": {4, true},
	"149.154.166.121": {4, true}, "149.154.167.118": {4, true},
	"149.154.165.111": {4, true},
	"91.108.56.100":   {5, false}, "91.108.56.101": {5, false},
	"91.108.56.116": {5, false}, "91.108.56.126": {5, false},
	"149.154.171.5": {5, false},
	"91.108.56.102": {5, true}, "91.108.56.128": {5, true},
	"91.108.56.151":  {5, true},
	"91.105.192.100": {203, false},
}

// dcOverrides maps special DC ids to relay DC (e.g. 203 -> 2).
var dcOverrides = map[int]int{
	203: 2,
}

// WsDomains returns kws hostnames for WebSocket upgrade, order matches Python _ws_domains.
func WsDomains(dc int, isMedia bool) []string {
	if o, ok := dcOverrides[dc]; ok {
		dc = o
	}
	s := strconv.Itoa(dc)
	if isMedia {
		return []string{
			"kws" + s + "-1.web.telegram.org",
			"kws" + s + ".web.telegram.org",
		}
	}
	return []string{
		"kws" + s + ".web.telegram.org",
		"kws" + s + "-1.web.telegram.org",
	}
}
