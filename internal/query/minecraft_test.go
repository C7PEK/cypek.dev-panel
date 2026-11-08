package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

// fakeMinecraft answers one status handshake like a real server.
func fakeMinecraft(t *testing.T, status any) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				hs, err := readPacket(conn) // handshake
				if err != nil {
					return
				}
				r := bytes.NewReader(hs)
				if id, _ := readVarInt(r); id != 0 {
					return
				}
				if proto, _ := readVarInt(r); proto != -1 {
					return
				}
				if _, err := readPacket(conn); err != nil { // status request
					return
				}
				js, _ := json.Marshal(status)
				var payload bytes.Buffer
				writeVarInt(&payload, 0)
				writeString(&payload, string(js))
				_ = writePacket(conn, payload.Bytes())
			}()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func TestMinecraftPing(t *testing.T) {
	host, port := fakeMinecraft(t, map[string]any{
		"version": map[string]any{"name": "Paper 26.3"},
		"players": map[string]any{"online": 2, "max": 20, "sample": []map[string]string{{"name": "cypek", "id": "x"}, {"name": "steve", "id": "y"}}},
	})
	p, err := Query(context.Background(), Spec{Kind: "minecraft-ping", Host: host, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if p.Online != 2 || p.Max != 20 || len(p.Names) != 2 || p.Names[0] != "cypek" {
		t.Errorf("players = %+v", p)
	}
	if _, err := Query(context.Background(), Spec{Kind: "nope"}); err == nil {
		t.Error("unknown kind accepted")
	}
	if _, err := Query(context.Background(), Spec{Kind: "minecraft-ping", Host: "127.0.0.1", Port: 1}); err == nil {
		t.Error("closed port succeeded")
	}
}

func TestCache(t *testing.T) {
	host, port := fakeMinecraft(t, map[string]any{"players": map[string]any{"online": 1, "max": 5}})
	c := NewCache(time.Minute)
	spec := Spec{Kind: "minecraft-ping", Host: host, Port: port}
	p1, err := c.Get(context.Background(), "s", spec)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := c.Get(context.Background(), "s", spec)
	if p1 != p2 {
		t.Error("second call not served from cache")
	}
	c.Forget("s")
	p3, _ := c.Get(context.Background(), "s", spec)
	if p3 == p1 {
		t.Error("Forget did not drop the entry")
	}
}

func TestVarInt(t *testing.T) {
	for _, v := range []int32{0, 1, 127, 128, 255, 300, 25565, -1, 2147483647} {
		var b bytes.Buffer
		writeVarInt(&b, v)
		got, err := readVarInt(bytes.NewReader(b.Bytes()))
		if err != nil || got != v {
			t.Errorf("roundtrip %d = %d, %v", v, got, err)
		}
	}
}
