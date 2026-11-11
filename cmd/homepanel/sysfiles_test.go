package main

import (
	"os"
	"testing"
)

func TestEmbeddedSystemFilesMatchPackaging(t *testing.T) {
	pairs := map[string]string{
		"systemfiles/homepanel.service":                  "../../packaging/systemd/homepanel.service",
		"systemfiles/homepanel-server@.service":          "../../packaging/systemd/homepanel-server@.service",
		"systemfiles/50-homepanel.rules":                 "../../packaging/polkit/50-homepanel.rules",
		"systemfiles/homepanel-database-install.service": "../../packaging/systemd/homepanel-database-install.service",
	}
	for embedded, src := range pairs {
		a, err := systemFiles.ReadFile(embedded)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s differs from %s; run: cp %s cmd/homepanel/%s", embedded, src, src, embedded)
		}
	}
}
