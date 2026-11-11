package netinfo

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

type Bound struct {
	TCP map[int]bool
	UDP map[int]bool
}

func (b Bound) Has(port int, proto string) bool {
	if strings.Contains(proto, "tcp") && b.TCP[port] {
		return true
	}
	return strings.Contains(proto, "udp") && b.UDP[port]
}

var procNet = "/proc/net"

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

func parseProcNet(r io.Reader, into map[int]bool, tcp bool) {
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}

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
