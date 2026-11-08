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

func TestOpenPortCommands(t *testing.T) {
	got := OpenPortCommands(Firewall{Kind: "ufw"}, 30120, "tcp+udp")
	if len(got) != 2 || got[0] != "sudo ufw allow 30120/tcp" || got[1] != "sudo ufw allow 30120/udp" {
		t.Errorf("ufw = %v", got)
	}
	got = OpenPortCommands(Firewall{Kind: "firewalld"}, 25565, "tcp")
	if len(got) != 2 || got[0] != "sudo firewall-cmd --permanent --add-port=25565/tcp" {
		t.Errorf("firewalld = %v", got)
	}
	if got := OpenPortCommands(Firewall{Kind: "none"}, 1, "tcp"); len(got) != 0 {
		t.Errorf("none = %v", got)
	}
}
