package query

import (
	"context"
	"os"
	"strconv"
	"testing"
)

func TestLiveMinecraft(t *testing.T) {
	port, _ := strconv.Atoi(os.Getenv("HOMEPANEL_QUERY_PORT"))
	if port == 0 {
		t.Skip("set HOMEPANEL_QUERY_PORT to ping a real server")
	}
	p, err := Query(context.Background(), Spec{Kind: "minecraft-ping", Host: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("players %d/%d names=%v", p.Online, p.Max, p.Names)
}
