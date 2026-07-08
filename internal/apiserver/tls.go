package apiserver

// TLS for the control plane. The design keeps the private key STABLE on disk and
// regenerates a short-lived-free, self-signed leaf certificate in memory on every
// start. Because the key never changes, the public-key pin a client fixes
// (curl --pinnedpubkey sha256//<pin>) survives every certificate regeneration —
// so changing the LAN IP (new SAN) or restarting never breaks a pinned client,
// and there is no certificate file to manage on disk.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const keyFileName = "server.key"

// loadOrCreateKey loads the stable ECDSA P-256 key from dir/server.key, creating
// and persisting one (0600) on first use. Keeping the key stable is what makes
// the public-key pin durable across certificate regenerations.
func loadOrCreateKey(dir string) (*ecdsa.PrivateKey, error) {
	p := filepath.Join(dir, keyFileName)
	if data, err := os.ReadFile(p); err == nil {
		if block, _ := pem.Decode(data); block != nil {
			if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
				return key, nil
			}
		}
		// unreadable/corrupt — fall through and regenerate
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(p, pemBytes, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// mintCert makes a fresh self-signed leaf for the given SANs, signed by the
// stable key, valid for 10 years. Regenerating it does not change the pin.
func mintCert(key *ecdsa.PrivateKey, ips []net.IP, dnsNames []string) (tls.Certificate, []byte, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "go-notepad"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
		DNSNames:     dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
	return cert, certPEM, nil
}

// publicKeyPin returns base64(SHA-256(SubjectPublicKeyInfo)) — the value a
// client pins. It depends only on the key, so it is stable across cert regens.
func publicKeyPin(key *ecdsa.PrivateKey) (string, error) {
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(spki)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

// sanIPs returns the IPs the certificate should cover: loopback always, plus the
// machine's LAN IP when the server is bound to all interfaces.
func sanIPs(allowlist []string) []net.IP {
	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	if BindHost(allowlist) == "0.0.0.0" {
		if ip := net.ParseIP(OutboundIP()); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}
