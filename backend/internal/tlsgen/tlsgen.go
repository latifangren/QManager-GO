// Package tlsgen provides self-signed TLS certificate generation for QManager Go Edition.
package tlsgen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureCertificates checks if TLS cert and key exist at certDir.
// If missing, it generates a self-signed ECDSA P-256 certificate with local IP/DNS SANs.
func EnsureCertificates(certDir string) (certPath string, keyPath string, err error) {
	if certDir == "" {
		certDir = "/etc/qmanager/tls"
	}

	certPath = filepath.Join(certDir, "cert.pem")
	keyPath = filepath.Join(certDir, "key.pem")

	if fileExistsAndNotEmpty(certPath) && fileExistsAndNotEmpty(keyPath) {
		return certPath, keyPath, nil
	}

	if err := os.MkdirAll(certDir, 0755); err != nil {
		// Fallback to /usrdata/qmanager/tls if unwritable
		certDir = "/usrdata/qmanager/tls"
		certPath = filepath.Join(certDir, "cert.pem")
		keyPath = filepath.Join(certDir, "key.pem")
		if err := os.MkdirAll(certDir, 0755); err != nil {
			return "", "", fmt.Errorf("tlsgen: failed to create cert directory: %w", err)
		}
	}

	// Generate ECDSA P-256 private key
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("tlsgen: failed to generate private key: %w", err)
	}

	// Serial number
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNum, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", "", fmt.Errorf("tlsgen: failed to generate serial number: %w", err)
	}

	// Collect local network interface IPs for SANs
	ipSANs := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("192.168.225.1"),
		net.ParseIP("192.168.8.1"),
		net.ParseIP("192.168.1.1"),
		net.ParseIP("::1"),
	}
	if ifaceIPs, err := getInterfaceIPs(); err == nil {
		ipSANs = append(ipSANs, ifaceIPs...)
	}

	template := x509.Certificate{
		SerialNumber: serialNum,
		Subject: pkix.Name{
			Organization: []string{"QManager"},
			CommonName:   "qmanager.local",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Valid for 10 years
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "qmanager.local", "modem.local"},
		IPAddresses:           ipSANs,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return "", "", fmt.Errorf("tlsgen: failed to create certificate: %w", err)
	}

	// Write Certificate PEM
	certOut, err := os.OpenFile(certPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return "", "", fmt.Errorf("tlsgen: failed to open cert file for writing: %w", err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return "", "", fmt.Errorf("tlsgen: failed to write cert pem: %w", err)
	}

	// Write Private Key PEM
	keyOut, err := os.OpenFile(keyPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", "", fmt.Errorf("tlsgen: failed to open key file for writing: %w", err)
	}
	defer keyOut.Close()

	keyBytes, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return "", "", fmt.Errorf("tlsgen: failed to marshal key: %w", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return "", "", fmt.Errorf("tlsgen: failed to write key pem: %w", err)
	}

	return certPath, keyPath, nil
}

func fileExistsAndNotEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func getInterfaceIPs() ([]net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && !ip.IsLoopback() {
				ips = append(ips, ip)
			}
		}
	}
	return ips, nil
}
