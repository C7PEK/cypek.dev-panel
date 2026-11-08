package netinfo

import (
	"net"
	"reflect"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		iface string
		ip    string
		want  string
	}{
		{"eth0", "192.168.1.10", "lan"},
		{"ens18", "10.0.0.5", "lan"},
		{"tailscale0", "100.101.102.103", "tailscale"},
		{"ts0", "100.64.0.1", "tailscale"},
		{"eth0", "203.0.113.5", "other"},
	}
	for _, c := range cases {
		if got := classify(c.iface, net.ParseIP(c.ip)); got != c.want {
			t.Errorf("classify(%s, %s) = %s, want %s", c.iface, c.ip, got, c.want)
		}
	}
}

func TestURLs(t *testing.T) {
	addrs := []Address{{IP: "192.168.1.10"}, {IP: "100.64.0.1"}}
	got := URLs(":8420", false, addrs)
	want := []string{"http://192.168.1.10:8420", "http://100.64.0.1:8420"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("URLs = %v, want %v", got, want)
	}
	got = URLs("127.0.0.1:8420", true, addrs)
	if !reflect.DeepEqual(got, []string{"https://127.0.0.1:8420"}) {
		t.Errorf("bound URLs = %v", got)
	}
	if got := URLs(":8420", false, nil); len(got) != 1 {
		t.Errorf("fallback URLs = %v", got)
	}
	if got := URLs("garbage", false, nil); got != nil {
		t.Errorf("bad listen = %v", got)
	}
}
