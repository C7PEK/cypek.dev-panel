package netinfo

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// Bound lists ports something on this host is bound to, per protocol
// ("tcp" listeners and bound "udp" sockets), read from /proc/net. Reading
// these files needs no privileges.
type Bound struct {
	TCP map[int]bool
	UDP map[int]bool
}

// Has reports whether port is bound for any protocol in proto
// ("tcp", "udp" or "tcp+udp").
func (b Bound) Has(port int, proto string) bool {
	if strings.Contains(proto, "tcp") && b.TCP[port] {
		return true
	}
	return strings.Contains(proto, "udp") && b.UDP[port]
}

// procNet is swapped in tests.
var procNet = "/proc/net"

// BoundPorts reads the host's listening TCP and bound UDP ports.
func BoundPorts() (Bound, error) {
	b := Bound{TCP: map[int]bool{}, UDP: map[int]bool{}}
	var firstErr error
	for _, f := range []struct {
		name string
		into map[int]bool
		tcp  bool
	}{{"tcp", b.TCP, true}, {"tcp6", b.TCP, true}, {"udp", b.UDP, false}, {"udp6", b.UDP, false}} {
		fh, err := os.Open(procNet + "/" + f.name)
		if err != nil {
			if firstErr == nil && !os.IsNotExist(err) {
				firstErr = err
			}
			continue
		}
		parseProcNet(fh, f.into, f.tcp)
		fh.Close()
	}
	return b, firstErr
}

// parseProcNet reads lines like
// "0: 0100007F:1F90 00000000:0000 0A ..." (local address, remote, state).
func parseProcNet(r io.Reader, into map[int]bool, tcp bool) {
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first {
			first = false // header
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		// TCP: only LISTEN (0A); UDP: every bound socket (07 = unconnected).
		if tcp && fields[3] != "0A" {
			continue
		}
		i := strings.LastIndexByte(fields[1], ':')
		if i < 0 {
			continue
		}
		port, err := strconv.ParseUint(fields[1][i+1:], 16, 16)
		if err == nil && port > 0 {
			into[int(port)] = true
		}
	}
}
