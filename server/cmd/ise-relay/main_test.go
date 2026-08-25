package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRuntimeConfigDefaultsAndOverrides(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		setRuntimeConfigEnvironment(t)

		configuration, err := loadRuntimeConfig()
		if err != nil {
			t.Fatalf("load runtime configuration: %v", err)
		}
		if configuration.Address != ":18443" ||
			configuration.InternalAddress != ":18080" ||
			configuration.Retention != 15*time.Minute ||
			configuration.MaxJobs != 16 || configuration.MaxInFlight != 2 ||
			configuration.LogLevel != "info" {
			t.Fatalf("unexpected default configuration: %#v", configuration)
		}
	})

	t.Run("overrides", func(t *testing.T) {
		setRuntimeConfigEnvironment(t)
		t.Setenv("ISE_RELAY_ADDRESS", "127.0.0.1:19443")
		t.Setenv("ISE_RELAY_INTERNAL_ADDRESS", "127.0.0.1:19080")
		t.Setenv("ISE_RELAY_RETENTION", "30m")
		t.Setenv("ISE_RELAY_MAX_JOBS", "32")
		t.Setenv("ISE_RELAY_MAX_IN_FLIGHT", "4")
		t.Setenv("LOG_LEVEL", "debug")

		configuration, err := loadRuntimeConfig()
		if err != nil {
			t.Fatalf("load runtime configuration: %v", err)
		}
		if configuration.Address != "127.0.0.1:19443" ||
			configuration.InternalAddress != "127.0.0.1:19080" ||
			configuration.Retention != 30*time.Minute ||
			configuration.MaxJobs != 32 || configuration.MaxInFlight != 4 ||
			configuration.LogLevel != "debug" {
			t.Fatalf("unexpected overridden configuration: %#v", configuration)
		}
	})
}

func TestLoadRuntimeConfigRejectsUnsafeSettings(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*testing.T)
	}{
		{
			name: "missing TLS file",
			apply: func(t *testing.T) {
				t.Setenv("ISE_RELAY_SERVER_CERT_FILE", "")
			},
		},
		{
			name: "short retention",
			apply: func(t *testing.T) {
				t.Setenv("ISE_RELAY_RETENTION", "30s")
			},
		},
		{
			name: "non-positive max jobs",
			apply: func(t *testing.T) {
				t.Setenv("ISE_RELAY_MAX_JOBS", "0")
			},
		},
		{
			name: "excessive max jobs",
			apply: func(t *testing.T) {
				t.Setenv("ISE_RELAY_MAX_JOBS", "65")
			},
		},
		{
			name: "max in flight exceeds max jobs",
			apply: func(t *testing.T) {
				t.Setenv("ISE_RELAY_MAX_JOBS", "2")
				t.Setenv("ISE_RELAY_MAX_IN_FLIGHT", "3")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRuntimeConfigEnvironment(t)
			test.apply(t)
			if _, err := loadRuntimeConfig(); err == nil {
				t.Fatal("expected unsafe runtime configuration to be rejected")
			}
		})
	}
}

func setRuntimeConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"ISE_RELAY_ADDRESS",
		"ISE_RELAY_INTERNAL_ADDRESS",
		"ISE_RELAY_RETENTION",
		"ISE_RELAY_MAX_JOBS",
		"ISE_RELAY_MAX_IN_FLIGHT",
		"LOG_LEVEL",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("ISE_RELAY_SERVER_CERT_FILE", "/run/secrets/server.pem")
	t.Setenv("ISE_RELAY_SERVER_KEY_FILE", "/run/secrets/server-key.pem")
	t.Setenv("ISE_RELAY_CLIENT_CA_FILE", "/run/secrets/client-ca.pem")
}

func TestRelayTLSRequiresTrustedClientCertificate(t *testing.T) {
	caCertificate, caKey, caPEM := newTestCA(t, "relay-test-ca")
	serverCertificate, serverKey := newTestCertificate(
		t, caCertificate, caKey, 2, "relay-server",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		[]net.IP{net.ParseIP("127.0.0.1")},
	)
	clientCertificate, clientKey := newTestCertificate(
		t, caCertificate, caKey, 3, "staging-client",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil,
	)
	rogueCA, rogueCAKey, _ := newTestCA(t, "rogue-ca")
	rogueCertificate, rogueKey := newTestCertificate(
		t, rogueCA, rogueCAKey, 4, "rogue-client",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil,
	)

	directory := t.TempDir()
	serverCertificateFile := writeTestPEM(t, directory, "server.pem", serverCertificate)
	serverKeyFile := writeTestPEM(t, directory, "server-key.pem", serverKey)
	clientCAFile := filepath.Join(directory, "client-ca.pem")
	if err := os.WriteFile(clientCAFile, caPEM, 0o600); err != nil {
		t.Fatalf("write client CA: %v", err)
	}
	configuration, err := loadTLSConfig(runtimeConfig{
		ServerCertFile: serverCertificateFile,
		ServerKeyFile:  serverKeyFile,
		ClientCAFile:   clientCAFile,
	})
	if err != nil {
		t.Fatalf("load TLS configuration: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		},
	))
	server.TLS = configuration
	server.StartTLS()
	defer server.Close()

	rootCAs := x509.NewCertPool()
	rootCAs.AppendCertsFromPEM(caPEM)
	tests := []struct {
		name        string
		certificate *tls.Certificate
		wantSuccess bool
	}{
		{name: "missing certificate"},
		{name: "untrusted certificate", certificate: testTLSCertificate(t, rogueCertificate, rogueKey)},
		{name: "trusted certificate", certificate: testTLSCertificate(t, clientCertificate, clientKey), wantSuccess: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tlsConfiguration := &tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    rootCAs,
			}
			if test.certificate != nil {
				tlsConfiguration.Certificates = []tls.Certificate{*test.certificate}
			}
			client := &http.Client{
				Transport: &http.Transport{TLSClientConfig: tlsConfiguration},
			}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			response, err := client.Do(request)
			if !test.wantSuccess {
				if err == nil {
					response.Body.Close()
					t.Fatal("untrusted client unexpectedly passed mTLS")
				}
				return
			}
			if err != nil {
				t.Fatalf("trusted client failed mTLS: %v", err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("trusted client status = %d", response.StatusCode)
			}
		})
	}
}

func newTestCA(
	t *testing.T,
	commonName string,
) (*x509.Certificate, *rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	return certificate, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func newTestCertificate(
	t *testing.T,
	ca *x509.Certificate,
	caKey *rsa.PrivateKey,
	serial int64,
	commonName string,
	usages []x509.ExtKeyUsage,
	ipAddresses []net.IP,
) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate certificate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  usages,
		IPAddresses:  ipAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return certificatePEM, keyPEM
}

func writeTestPEM(t *testing.T, directory string, name string, value []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func testTLSCertificate(t *testing.T, certificate []byte, key []byte) *tls.Certificate {
	t.Helper()
	parsed, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		t.Fatalf("parse test certificate: %v", err)
	}
	return &parsed
}
