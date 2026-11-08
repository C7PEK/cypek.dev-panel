package supervisor

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestAddonStartAsPanelUser starts the database installer unit the way the
// panel does (opt-in: HOMEPANEL_ADDON_TEST=1, run as the homepanel user).
func TestAddonStartAsPanelUser(t *testing.T) {
	if os.Getenv("HOMEPANEL_ADDON_TEST") != "1" {
		t.Skip("set HOMEPANEL_ADDON_TEST=1")
	}
	ctx := context.Background()
	s, err := NewSystemd(ctx, Layout{ServersDir: "/var/lib/homepanel/servers", RunDir: "/run", FlatRun: true}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartAddon(ctx, "sshd.service"); err != ErrUnitNotAllowed {
		t.Errorf("non-addon unit: %v", err)
	}
	if err := s.StartAddon(ctx, "homepanel-database-install.service"); err != nil {
		t.Fatalf("StartAddon: %v", err)
	}
	for i := 0; i < 10; i++ {
		a, sub, err := s.AddonState(ctx, "homepanel-database-install.service")
		t.Logf("state %s/%s %v", a, sub, err)
		if a == "activating" || a == "active" || a == "inactive" && i > 0 {
			return
		}
		time.Sleep(time.Second)
	}
}
