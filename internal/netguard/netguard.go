package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

var ErrNotPublic = errors.New("not a public internet address")

var blocked = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"100.64.0.0/10", "198.18.0.0/15", "0.0.0.0/8", "192.0.0.0/24", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "2001:db8::/32"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

func Public(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	for _, n := range blocked {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

func control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if !Public(net.ParseIP(host)) {
		return fmt.Errorf("refusing to connect to %s: %w", host, ErrNotPublic)
	}
	return nil
}

type Options struct {
	Timeout time.Duration

	HTTPSOnly bool

	MaxRedirects int
}

func Client(o Options) *http.Client {
	if o.MaxRedirects == 0 {
		o.MaxRedirects = 10
	}
	return &http.Client{
		Timeout: o.Timeout,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, Control: control}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if o.HTTPSOnly && req.URL.Scheme != "https" {
				return errors.New("redirect to a non-https link")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return errors.New("redirect to an unsupported link")
			}
			if len(via) >= o.MaxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}
