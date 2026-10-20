package main

import (
	"crypto/x509"
	"encoding/pem"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxCertFiles = 500
	certCacheTTL = 10 * time.Minute
)

type certInfo struct {
	Name      string    `json:"name"`
	Names     []string  `json:"names"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	leaf      *x509.Certificate
}

// readCerts lists the leaf certificates Caddy stores under dir/certificates,
// keeping the newest certificate for each name.
func readCerts(dir string) ([]certInfo, error) {
	root := filepath.Join(dir, "certificates")
	if _, e := os.Stat(root); e != nil {
		return nil, e
	}
	best := map[string]certInfo{}
	seen := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".crt") || !d.Type().IsRegular() {
			return nil
		}
		if seen++; seen > maxCertFiles {
			return fs.SkipAll
		}
		f, e := os.Open(p)
		if e != nil {
			return nil
		}
		raw, e := io.ReadAll(io.LimitReader(f, 256<<10))
		f.Close()
		if e != nil {
			return nil
		}
		block, _ := pem.Decode(raw)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil
		}
		leaf, e := x509.ParseCertificate(block.Bytes)
		if e != nil {
			return nil
		}
		info := certInfo{Issuer: leaf.Issuer.CommonName, NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter, leaf: leaf}
		info.Names = append(info.Names, leaf.DNSNames...)
		for _, ip := range leaf.IPAddresses {
			info.Names = append(info.Names, ip.String())
		}
		switch {
		case len(info.Names) > 0:
			info.Name = info.Names[0]
		case leaf.Subject.CommonName != "":
			info.Name = leaf.Subject.CommonName
		default:
			return nil
		}
		if old, ok := best[info.Name]; !ok || info.NotAfter.After(old.NotAfter) {
			best[info.Name] = info
		}
		return nil
	})
	out := make([]certInfo, 0, len(best))
	for _, c := range best {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NotAfter.Before(out[j].NotAfter) })
	return out, nil
}

// hostOnly reduces a Caddy site address to a bare host name or IP.
func hostOnly(address string) string {
	h := strings.ToLower(strings.TrimSpace(address))
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexAny(h, "/?"); i >= 0 {
		h = h[:i]
	}
	if host, _, e := net.SplitHostPort(h); e == nil {
		return host
	}
	return strings.Trim(h, "[]")
}

// certServes reports whether the certificate covers one of the served hosts.
func certServes(c certInfo, hosts []string) bool {
	for _, raw := range hosts {
		h := hostOnly(raw)
		if h == "" {
			continue
		}
		if strings.HasPrefix(h, "*.") {
			for _, name := range c.Names {
				if strings.EqualFold(name, h) {
					return true
				}
			}
			continue
		}
		if c.leaf != nil && c.leaf.VerifyHostname(h) == nil {
			return true
		}
	}
	return false
}

// warnWindow is how close to expiry a certificate must be before it is flagged: the
// configured number of days. Short-lived certificates (under 30 days of validity,
// like the six-day ones Caddy gets for IP addresses) are flagged only in the last
// fifth of their life, otherwise they would be flagged all the time.
func warnWindow(c certInfo, days int) time.Duration {
	window := time.Duration(days) * 24 * time.Hour
	if life := c.NotAfter.Sub(c.NotBefore); life < 30*24*time.Hour && life/5 < window {
		window = life / 5
	}
	return window
}
