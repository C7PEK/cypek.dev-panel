package netinfo

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestParseProcNet(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644) }
	hdr := "  sl  local_address rem_address   st tx_queue rx_queue\n"
	write("tcp", hdr+"   0: 0100007F:1F90 00000000:0000 0A 0\n   1: 0100007F:7531 0A08010D:D431 01 0\n")
	write("tcp6", hdr+"   0: 00000000000000000000000000000000:20E4 00000000000000000000000000000000:0000 0A 0\n")
	write("udp", hdr+"   0: 00000000:7578 00000000:0000 07 0\n")
	old := procNet
	procNet = dir
	defer func() { procNet = old }()
	b, err := BoundPorts()
	if err != nil {
		t.Fatal(err)
	}
	if !b.TCP[8080] || b.TCP[30001] || !b.TCP[8420] || !b.UDP[30072] || b.UDP[8080] {
		t.Fatalf("%+v", b)
	}
	if !b.Has(30072, "tcp+udp") || b.Has(30072, "tcp") || !b.Has(8420, "tcp") {
		t.Error("Has")
	}
}

func TestBoundPortsLive(t *testing.T) {
	if _, err := os.Stat("/proc/net/tcp"); err != nil {
		t.Skip("no /proc/net")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	b, _ := BoundPorts()
	if !b.TCP[port] {
		t.Errorf("listener on %d not seen", port)
	}
}
