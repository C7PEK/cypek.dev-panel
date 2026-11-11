package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSelfSigned(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	cert, key, fp, err := EnsureSelfSigned(dir, []string{"192.168.8.111", "panel"})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	for _, h := range []string{"192.168.8.111", "panel", "localhost", "127.0.0.1"} {
		if err := leaf.VerifyHostname(h); err != nil {
			t.Errorf("%s: %v", h, err)
		}
	}
	if info, _ := os.Stat(key); info.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v", info.Mode())
	}

	_, _, fp2, _ := EnsureSelfSigned(dir, []string{"192.168.8.111"})
	if fp2 != fp {
		t.Error("certificate regenerated without need")
	}
	_, _, fp3, _ := EnsureSelfSigned(dir, []string{"10.0.0.5"})
	if fp3 == fp || len(fp3) != 95 {
		t.Errorf("not regenerated for a new IP: %s", fp3)
	}
}
