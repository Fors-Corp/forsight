package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

func metricsByName(metrics []model.Metric) map[string]model.Metric {
	out := make(map[string]model.Metric, len(metrics))
	for _, m := range metrics {
		out[m.Name] = m
	}
	return out
}

func TestCollect_PlainHTTP_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short circuit"))
	}))
	defer srv.Close()

	c := New([]Target{{URL: srv.URL, Name: "teapot"}}, 5*time.Second)
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	byName := metricsByName(metrics)

	up, ok := byName["probe.http.up"]
	if !ok || up.Value != 1 {
		t.Fatalf("probe.http.up = %+v, want 1", up)
	}
	if up.Labels["url"] != srv.URL || up.Labels["name"] != "teapot" {
		t.Errorf("probe.http.up labels = %+v", up.Labels)
	}

	status, ok := byName["probe.http.status"]
	if !ok || status.Value != http.StatusTeapot {
		t.Fatalf("probe.http.status = %+v, want %d", status, http.StatusTeapot)
	}

	duration, ok := byName["probe.http.duration_ms"]
	if !ok || duration.Value < 0 {
		t.Fatalf("probe.http.duration_ms = %+v, want a non-negative value", duration)
	}

	if _, ok := byName["probe.tls.valid"]; ok {
		t.Error("plain HTTP target should not produce TLS metrics")
	}
	if _, ok := byName["probe.tls.days_remaining"]; ok {
		t.Error("plain HTTP target should not produce TLS metrics")
	}
}

func TestCollect_UnreachableTarget_ReportsDownNotError(t *testing.T) {
	// Port 0 on loopback with nothing listening: connection refused, fast.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close() // now guaranteed nothing is listening there

	c := New([]Target{{URL: "http://" + addr, Name: "dead"}}, 2*time.Second)
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect returned an error for a down target, want up=0 instead: %v", err)
	}
	byName := metricsByName(metrics)

	up, ok := byName["probe.http.up"]
	if !ok || up.Value != 0 {
		t.Fatalf("probe.http.up = %+v, want 0", up)
	}
	if _, ok := byName["probe.http.status"]; ok {
		t.Error("a target with no response should not produce probe.http.status")
	}
	if _, ok := byName["probe.http.duration_ms"]; !ok {
		t.Error("probe.http.duration_ms should still be reported for a failed probe")
	}
}

func TestCollect_UnresolvableHost_LogsOnceNotEveryTick(t *testing.T) {
	target := Target{URL: "http://this-host-does-not-exist.invalid.", Name: "nowhere"}
	c := New([]Target{target}, time.Second)

	for i := 0; i < 3; i++ {
		metrics, err := c.Collect(context.Background())
		if err != nil {
			t.Fatalf("tick %d: Collect returned an error, want up=0: %v", i, err)
		}
		byName := metricsByName(metrics)
		if up := byName["probe.http.up"]; up.Value != 0 {
			t.Fatalf("tick %d: probe.http.up = %+v, want 0", i, up)
		}
	}

	if !c.warned[target.URL] {
		t.Error("an unresolvable host should be marked warned after its first tick")
	}
	if len(c.warned) != 1 {
		t.Errorf("warned = %v, want exactly one entry", c.warned)
	}
}

func TestCollect_TLS_ValidCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	c := New([]Target{{URL: srv.URL, Name: "secure"}}, 5*time.Second)
	c.rootCAs = pool

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	byName := metricsByName(metrics)

	if up := byName["probe.http.up"]; up.Value != 1 {
		t.Fatalf("probe.http.up = %+v, want 1", up)
	}
	valid, ok := byName["probe.tls.valid"]
	if !ok || valid.Value != 1 {
		t.Fatalf("probe.tls.valid = %+v, want 1 for a trusted certificate", valid)
	}
	days, ok := byName["probe.tls.days_remaining"]
	if !ok || days.Value <= 0 {
		t.Fatalf("probe.tls.days_remaining = %+v, want a positive value", days)
	}
}

func TestCollect_TLS_ExpiredCertificate(t *testing.T) {
	certDER, key := selfSignedExpiredCert(t)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{certDER}, PrivateKey: key}},
	}
	srv.StartTLS()
	defer srv.Close()

	c := New([]Target{{URL: srv.URL, Name: "stale-cert"}}, 5*time.Second)
	// rootCAs left nil (system pool) deliberately: an expired certificate
	// fails verification on time alone, trusted issuer or not.

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	byName := metricsByName(metrics)

	// The site itself is still reachable — an expired cert must not make
	// probe.http.up lie about that; see the package doc.
	if up := byName["probe.http.up"]; up.Value != 1 {
		t.Fatalf("probe.http.up = %+v, want 1 (host is reachable even with a bad cert)", up)
	}
	valid, ok := byName["probe.tls.valid"]
	if !ok || valid.Value != 0 {
		t.Fatalf("probe.tls.valid = %+v, want 0 for an expired certificate", valid)
	}
	days, ok := byName["probe.tls.days_remaining"]
	if !ok || days.Value >= 0 {
		t.Fatalf("probe.tls.days_remaining = %+v, want a negative value for an expired certificate", days)
	}
}

// selfSignedExpiredCert builds a certificate (self-signed, so it is its own
// issuer) whose validity window already closed a day ago, covering
// 127.0.0.1 the way httptest's own Listener binds.
func selfSignedExpiredCert(t *testing.T) (certDER []byte, key *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-48 * time.Hour),
		NotAfter:              time.Now().Add(-24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return der, priv
}
