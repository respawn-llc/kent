// Package proxyfixture supplies local TLS peers for proxy transport tests.
package proxyfixture

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type TLS struct {
	ProxyURL      string
	TargetURL     string
	Roots         *x509.CertPool
	ProxyCalls    atomic.Int32
	TargetCalls   atomic.Int32
	proxyAddress  string
	targetAddress string
}

// NewTLS issues distinct hostname-specific certificates from one test CA.
// Changing either certificate name exercises verification, not issuer trust.
func NewTLS(t testing.TB, handler http.Handler, proxyCertificateName, targetCertificateName string, proxyTLS bool) *TLS {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "proxy fixture CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	certificate := func(name string, serial int64) tls.Certificate {
		leaf := &x509.Certificate{
			SerialNumber: big.NewInt(serial), DNSNames: []string{name},
			NotBefore: ca.NotBefore, NotAfter: ca.NotAfter,
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: key}
	}
	fixture := &TLS{Roots: roots}
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.TargetCalls.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to destination")
		}
		handler.ServeHTTP(w, r)
	}))
	target.TLS = &tls.Config{Certificates: []tls.Certificate{certificate(targetCertificateName, 2)}}
	target.StartTLS()
	t.Cleanup(target.Close)
	fixture.targetAddress = target.Listener.Addr().String()
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.ProxyCalls.Add(1)
		if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Error("CONNECT missing proxy credentials")
		}
		if r.Method != http.MethodConnect || r.Host != "destination.test:443" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		upstream, err := net.Dial("tcp", fixture.targetAddress)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()
		_, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if err != nil {
			t.Error(err)
			return
		}
		if err := buffer.Flush(); err != nil {
			t.Error(err)
			return
		}
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, buffer)
			_ = upstream.Close()
			close(done)
		}()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		<-done
	}))
	proxy.TLS = &tls.Config{Certificates: []tls.Certificate{certificate(proxyCertificateName, 3)}}
	scheme := "http"
	if proxyTLS {
		proxy.StartTLS()
		scheme = "https"
	} else {
		proxy.Start()
	}
	t.Cleanup(proxy.Close)
	fixture.proxyAddress = proxy.Listener.Addr().String()
	fixture.ProxyURL = scheme + "://user:pass@proxy.test:" + port(t, proxy.URL)
	fixture.TargetURL = "https://destination.test/"
	return fixture
}

func port(t testing.TB, address string) string {
	t.Helper()
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Port()
}

func (f *TLS) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	switch host {
	case "proxy.test":
		address = f.proxyAddress
	case "destination.test":
		address = f.targetAddress
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
