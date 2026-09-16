package cmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
	"github.com/marcfs31/forsight/forsight/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewBackingStore(t *testing.T) {
	t.Run("defaults to memory", func(t *testing.T) {
		backing, badgerStore, err := newBackingStore(&runOptions{storeBackend: "", retention: time.Hour})
		if err != nil {
			t.Fatalf("newBackingStore: %v", err)
		}
		if badgerStore != nil {
			t.Errorf("badgerStore = %v, want nil for an unset --store", badgerStore)
		}
		if _, ok := backing.(*store.MemoryStore); !ok {
			t.Errorf("backing = %T, want *store.MemoryStore", backing)
		}
	})

	t.Run(`"memory" is explicit too`, func(t *testing.T) {
		backing, badgerStore, err := newBackingStore(&runOptions{storeBackend: "memory", retention: time.Hour})
		if err != nil {
			t.Fatalf("newBackingStore: %v", err)
		}
		if badgerStore != nil {
			t.Errorf("badgerStore = %v, want nil for --store=memory", badgerStore)
		}
		if _, ok := backing.(*store.MemoryStore); !ok {
			t.Errorf("backing = %T, want *store.MemoryStore", backing)
		}
	})

	t.Run(`"badger" opens a database at --data-dir and returns it for the shutdown path too`, func(t *testing.T) {
		dir := t.TempDir()
		backing, badgerStore, err := newBackingStore(&runOptions{storeBackend: "badger", dataDir: dir, retention: time.Hour})
		if err != nil {
			t.Fatalf("newBackingStore: %v", err)
		}
		t.Cleanup(func() {
			if err := closeBadgerStore(badgerStore, discardLogger()); err != nil {
				t.Errorf("closeBadgerStore: %v", err)
			}
		})
		if badgerStore == nil {
			t.Fatal("badgerStore = nil, want the opened *store.BadgerStore for --store=badger")
		}
		if backing != store.Store(badgerStore) {
			t.Errorf("backing and badgerStore must be the same value: backing=%v badgerStore=%v", backing, badgerStore)
		}
	})

	t.Run("rejects an unknown backend", func(t *testing.T) {
		_, _, err := newBackingStore(&runOptions{storeBackend: "postgres", retention: time.Hour})
		if err == nil {
			t.Fatal(`newBackingStore(storeBackend: "postgres") = nil error, want one`)
		}
	})
}

func TestCloseBadgerStore_NilIsNoop(t *testing.T) {
	if err := closeBadgerStore(nil, discardLogger()); err != nil {
		t.Errorf("closeBadgerStore(nil, ...) = %v, want nil", err)
	}
}

func TestResolveAuthToken(t *testing.T) {
	t.Setenv("FORSIGHT_AUTH_TOKEN", "from-env")

	if got := resolveAuthToken("from-flag"); got != "from-flag" {
		t.Errorf("flag should override env: got %q", got)
	}
	if got := resolveAuthToken(""); got != "from-env" {
		t.Errorf("empty flag should fall back to env: got %q", got)
	}

	t.Setenv("FORSIGHT_AUTH_TOKEN", "")
	if got := resolveAuthToken(""); got != "" {
		t.Errorf("empty flag and empty env: got %q, want empty", got)
	}
}

func TestResolveErrorSLO(t *testing.T) {
	t.Setenv("FORSIGHT_ERROR_SLO", "0.02")

	if got := resolveErrorSLO(0.05); got != 0.05 {
		t.Errorf("flag should override env: got %v", got)
	}
	if got := resolveErrorSLO(0); got != 0.02 {
		t.Errorf("unset flag should fall back to env: got %v", got)
	}

	t.Setenv("FORSIGHT_ERROR_SLO", "not-a-number")
	if got := resolveErrorSLO(0); got != 0 {
		t.Errorf("unparsable env should be ignored: got %v, want 0", got)
	}

	t.Setenv("FORSIGHT_ERROR_SLO", "")
	if got := resolveErrorSLO(0); got != 0 {
		t.Errorf("unset flag and empty env: got %v, want 0", got)
	}
	if got := resolveErrorSLO(-1); got != 0 {
		t.Errorf("non-positive flag and empty env: got %v, want 0", got)
	}
}

func TestResolveMlaas(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		t.Setenv("MLAAS_URL", "")
		t.Setenv("MLAAS_API_KEY", "")
		t.Setenv("MLAAS_API_KEY_FILE", "")
		_, ok, err := resolveMlaas(&runOptions{})
		if err != nil || ok {
			t.Fatalf("resolveMlaas with nothing set = ok %v, err %v; want off and no error", ok, err)
		}
	})

	t.Run("key file, trimmed", func(t *testing.T) {
		t.Setenv("MLAAS_API_KEY", "")
		path := filepath.Join(t.TempDir(), "api_key")
		if err := os.WriteFile(path, []byte("  secret-token\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, ok, err := resolveMlaas(&runOptions{
			mlaasURL: "http://127.0.0.1:8090", mlaasAPIKeyFile: path,
			mlaasPrefix: "agent-a", mlaasSyncInterval: time.Minute,
		})
		if err != nil || !ok {
			t.Fatalf("resolveMlaas = ok %v, err %v; want on", ok, err)
		}
		if cfg.APIKey != "secret-token" {
			t.Errorf("APIKey = %q, want the file's contents trimmed", cfg.APIKey)
		}
		if cfg.URL != "http://127.0.0.1:8090" || cfg.Prefix != "agent-a" || cfg.SyncInterval != time.Minute {
			t.Errorf("cfg = %+v, want the flags carried through", cfg)
		}
	})

	t.Run("env wins over the file and fills in the URL", func(t *testing.T) {
		t.Setenv("MLAAS_URL", "http://mlaas.internal:8090")
		t.Setenv("MLAAS_API_KEY", "from-env")
		cfg, ok, err := resolveMlaas(&runOptions{mlaasAPIKeyFile: filepath.Join(t.TempDir(), "missing")})
		if err != nil || !ok {
			t.Fatalf("resolveMlaas = ok %v, err %v; want on", ok, err)
		}
		if cfg.URL != "http://mlaas.internal:8090" || cfg.APIKey != "from-env" {
			t.Errorf("cfg = %+v, want URL and key from the environment", cfg)
		}
	})

	t.Run("a URL without a key is an error up front", func(t *testing.T) {
		t.Setenv("MLAAS_API_KEY", "")
		t.Setenv("MLAAS_API_KEY_FILE", "")
		if _, _, err := resolveMlaas(&runOptions{mlaasURL: "http://127.0.0.1:8090"}); err == nil {
			t.Fatal("resolveMlaas with a URL and no key: want an error, got nil")
		}
		missing := filepath.Join(t.TempDir(), "nope")
		if _, _, err := resolveMlaas(&runOptions{mlaasURL: "http://127.0.0.1:8090", mlaasAPIKeyFile: missing}); err == nil {
			t.Fatal("resolveMlaas with an unreadable key file: want an error, got nil")
		}
	})
}

func TestIsLoopbackListenAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{":8080", false},
		{"0.0.0.0:8080", false},
		{"192.168.1.1:8080", false},
	}
	for _, tc := range cases {
		if got := isLoopbackListenAddr(tc.addr); got != tc.want {
			t.Errorf("isLoopbackListenAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

// TestNewHTTPServer_SetsEveryTimeout guards against the listener quietly
// going back to a bare &http.Server{}: with no timeouts a client that never
// finishes its headers holds a goroutine forever. WriteTimeout must cover
// ReadTimeout because Go starts it once the header is read, so it spans the
// body too.
func TestNewHTTPServer_SetsEveryTimeout(t *testing.T) {
	handler := http.NewServeMux()
	srv := newHTTPServer("127.0.0.1:0", handler)

	if srv.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q, want 127.0.0.1:0", srv.Addr)
	}
	if srv.Handler != handler {
		t.Error("Handler was not the one passed in")
	}
	for name, d := range map[string]time.Duration{
		"ReadHeaderTimeout": srv.ReadHeaderTimeout,
		"ReadTimeout":       srv.ReadTimeout,
		"WriteTimeout":      srv.WriteTimeout,
		"IdleTimeout":       srv.IdleTimeout,
	} {
		if d <= 0 {
			t.Errorf("%s = %v, want > 0", name, d)
		}
	}
	if srv.ReadHeaderTimeout > srv.ReadTimeout {
		t.Errorf("ReadHeaderTimeout %v exceeds ReadTimeout %v", srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
	if srv.WriteTimeout < srv.ReadTimeout {
		t.Errorf("WriteTimeout %v is shorter than ReadTimeout %v; a large OTLP upload would be cut off mid-body", srv.WriteTimeout, srv.ReadTimeout)
	}
	if srv.MaxHeaderBytes <= 0 {
		t.Errorf("MaxHeaderBytes = %d, want > 0", srv.MaxHeaderBytes)
	}
}

func TestParseScrapeTargets(t *testing.T) {
	t.Run("bare URL labels the job with the host", func(t *testing.T) {
		got, err := parseScrapeTargets([]string{"http://localhost:9100/metrics"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 target, got %d", len(got))
		}
		if got[0].URL != "http://localhost:9100/metrics" {
			t.Errorf("URL = %q", got[0].URL)
		}
		if got[0].Labels["job"] != "localhost:9100" {
			t.Errorf(`job = %q, want "localhost:9100"`, got[0].Labels["job"])
		}
	})

	t.Run("job= prefix wins over the host default", func(t *testing.T) {
		got, err := parseScrapeTargets([]string{"node=http://10.0.0.4:9100/metrics"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[0].Labels["job"] != "node" {
			t.Errorf(`job = %q, want "node"`, got[0].Labels["job"])
		}
		if got[0].URL != "http://10.0.0.4:9100/metrics" {
			t.Errorf("URL = %q — the prefix must not stay in the URL", got[0].URL)
		}
	})

	t.Run("a URL containing = is not mistaken for a job prefix", func(t *testing.T) {
		got, err := parseScrapeTargets([]string{"http://host:9100/metrics?format=text"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[0].URL != "http://host:9100/metrics?format=text" {
			t.Errorf("URL = %q", got[0].URL)
		}
		if got[0].Labels["job"] != "host:9100" {
			t.Errorf(`job = %q`, got[0].Labels["job"])
		}
	})

	t.Run("rejects a value that is not an absolute URL", func(t *testing.T) {
		for _, bad := range []string{"localhost:9100/metrics", "job=", "/metrics"} {
			if _, err := parseScrapeTargets([]string{bad}); err == nil {
				t.Errorf("parseScrapeTargets(%q) = nil error, want one", bad)
			}
		}
	})

	t.Run("no targets is not an error", func(t *testing.T) {
		got, err := parseScrapeTargets(nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v", got, err)
		}
	})
}

// errSink always fails WriteLogs, forcing filelog.Tail to return an error
// so retryTail's restart path runs.
type errSink struct{ calls atomic.Int32 }

func (s *errSink) WriteLogs(_ context.Context, _ []model.LogEntry) error {
	s.calls.Add(1)
	return errors.New("boom")
}

// TestRetryTail_RestartsAfterFailureAndStopsOnCancel guards against the
// finding that a filelog.Tail failure used to kill the collector for a
// path permanently ("log tailer stopped" was the last anyone heard of it,
// logged once by cmd/run.go's goroutine before it exited for good). A
// failing tailer must now be restarted with backoff — and still exit
// promptly once the context is cancelled, rather than retrying forever.
func TestRetryTail_RestartsAfterFailureAndStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sink := &errSink{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		retryTail(ctx, path, sink, nil, logger, time.Millisecond, 5*time.Millisecond)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for sink.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := sink.calls.Load(); got < 3 {
		t.Fatalf("want at least 3 restarts (WriteLogs calls), got %d — retryTail should keep restarting a failing tailer", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retryTail did not stop promptly after ctx cancellation")
	}
}

func TestResolveTLSConfig(t *testing.T) {
	certPEM, keyPEM := newSelfSignedCert(t, "forsight-test")
	dir := t.TempDir()
	certPath := writeTestFile(t, dir, "server.crt", certPEM)
	keyPath := writeTestFile(t, dir, "server.key", keyPEM)

	t.Run("off by default", func(t *testing.T) {
		t.Setenv("FORSIGHT_TLS_CERT", "")
		t.Setenv("FORSIGHT_TLS_KEY", "")
		t.Setenv("FORSIGHT_TLS_CLIENT_CA", "")
		cfg, ok, err := resolveTLSConfig(&runOptions{})
		if err != nil || ok || cfg != nil {
			t.Fatalf("resolveTLSConfig with nothing set = cfg %v, ok %v, err %v; want nil, false, nil", cfg, ok, err)
		}
	})

	t.Run("cert and key from flags enable TLS", func(t *testing.T) {
		cfg, ok, err := resolveTLSConfig(&runOptions{tlsCertFile: certPath, tlsKeyFile: keyPath})
		if err != nil || !ok {
			t.Fatalf("resolveTLSConfig = ok %v, err %v; want on", ok, err)
		}
		if len(cfg.Certificates) != 1 {
			t.Fatalf("Certificates = %d entries, want 1", len(cfg.Certificates))
		}
		if cfg.ClientAuth != tls.NoClientCert {
			t.Errorf("ClientAuth = %v, want NoClientCert without --tls-client-ca", cfg.ClientAuth)
		}
		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %#x, want TLS 1.2", cfg.MinVersion)
		}
		if cfg.ClientCAs != nil {
			t.Error("ClientCAs is set without --tls-client-ca")
		}
	})

	t.Run("env fallback for both cert and key", func(t *testing.T) {
		t.Setenv("FORSIGHT_TLS_CERT", certPath)
		t.Setenv("FORSIGHT_TLS_KEY", keyPath)
		cfg, ok, err := resolveTLSConfig(&runOptions{})
		if err != nil || !ok || len(cfg.Certificates) != 1 {
			t.Fatalf("resolveTLSConfig via env = cfg %v, ok %v, err %v", cfg, ok, err)
		}
	})

	t.Run("the flag wins over the env fallback", func(t *testing.T) {
		t.Setenv("FORSIGHT_TLS_CERT", filepath.Join(dir, "does-not-exist.crt"))
		t.Setenv("FORSIGHT_TLS_KEY", filepath.Join(dir, "does-not-exist.key"))
		cfg, ok, err := resolveTLSConfig(&runOptions{tlsCertFile: certPath, tlsKeyFile: keyPath})
		if err != nil || !ok || len(cfg.Certificates) != 1 {
			t.Fatalf("resolveTLSConfig with flag and env both set = cfg %v, ok %v, err %v; want the flag's files used", cfg, ok, err)
		}
	})

	t.Run("only --tls-cert set is an error", func(t *testing.T) {
		if _, ok, err := resolveTLSConfig(&runOptions{tlsCertFile: certPath}); err == nil || ok {
			t.Fatalf("resolveTLSConfig with only --tls-cert = ok %v, err %v; want an error and ok=false", ok, err)
		}
	})

	t.Run("only --tls-key set is an error", func(t *testing.T) {
		if _, ok, err := resolveTLSConfig(&runOptions{tlsKeyFile: keyPath}); err == nil || ok {
			t.Fatalf("resolveTLSConfig with only --tls-key = ok %v, err %v; want an error and ok=false", ok, err)
		}
	})

	t.Run("a missing --tls-cert path is an error, not a plaintext fallback", func(t *testing.T) {
		_, ok, err := resolveTLSConfig(&runOptions{tlsCertFile: filepath.Join(dir, "missing.crt"), tlsKeyFile: keyPath})
		if err == nil || ok {
			t.Fatalf("resolveTLSConfig with a missing cert = ok %v, err %v; want an error and ok=false", ok, err)
		}
	})

	t.Run("a mismatched cert/key pair is an error", func(t *testing.T) {
		otherCertPEM, _ := newSelfSignedCert(t, "someone-else")
		otherCertPath := writeTestFile(t, dir, "other.crt", otherCertPEM)
		if _, ok, err := resolveTLSConfig(&runOptions{tlsCertFile: otherCertPath, tlsKeyFile: keyPath}); err == nil || ok {
			t.Fatalf("resolveTLSConfig with a mismatched cert/key = ok %v, err %v; want an error and ok=false", ok, err)
		}
	})

	t.Run("--tls-client-ca enables mTLS", func(t *testing.T) {
		_, _, caPEM := newTestCA(t)
		caPath := writeTestFile(t, dir, "ca.crt", caPEM)
		cfg, ok, err := resolveTLSConfig(&runOptions{tlsCertFile: certPath, tlsKeyFile: keyPath, tlsClientCAFile: caPath})
		if err != nil || !ok {
			t.Fatalf("resolveTLSConfig = ok %v, err %v; want on", ok, err)
		}
		if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
			t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
		}
		if cfg.ClientCAs == nil {
			t.Error("ClientCAs = nil, want the parsed --tls-client-ca pool")
		}
	})

	t.Run("--tls-client-ca with no PEM certificate in it is an error", func(t *testing.T) {
		badPath := writeTestFile(t, dir, "bad-ca.crt", []byte("not a certificate"))
		if _, ok, err := resolveTLSConfig(&runOptions{
			tlsCertFile: certPath, tlsKeyFile: keyPath, tlsClientCAFile: badPath,
		}); err == nil || ok {
			t.Fatalf("resolveTLSConfig with a bad --tls-client-ca = ok %v, err %v; want an error and ok=false", ok, err)
		}
	})

	t.Run("a missing --tls-client-ca file is an error", func(t *testing.T) {
		if _, ok, err := resolveTLSConfig(&runOptions{
			tlsCertFile: certPath, tlsKeyFile: keyPath, tlsClientCAFile: filepath.Join(dir, "missing-ca.crt"),
		}); err == nil || ok {
			t.Fatalf("resolveTLSConfig with a missing --tls-client-ca = ok %v, err %v; want an error and ok=false", ok, err)
		}
	})
}

// TestTLSConfig_Handshake proves resolveTLSConfig's *tls.Config is not just
// shaped right but actually enforces what it promises over a real
// connection: plain TLS lets any client in, while setting --tls-client-ca
// refuses a client with no certificate and accepts one signed by that CA.
// This is the config run.go hands straight to http.Server.TLSConfig ahead of
// ListenAndServeTLS("", ""); a wiring slip here (e.g. the wrong ClientAuth
// mode) would let an unauthenticated client through mTLS silently, which a
// struct-field-only assertion would not catch.
func TestTLSConfig_Handshake(t *testing.T) {
	ca, caKey, caPEM := newTestCA(t)
	serverCertPEM, serverKeyPEM := newSignedCert(t, ca, caKey, "127.0.0.1", []net.IP{net.ParseIP("127.0.0.1")}, x509.ExtKeyUsageServerAuth)
	clientCertPEM, clientKeyPEM := newSignedCert(t, ca, caKey, "test-client", nil, x509.ExtKeyUsageClientAuth)

	dir := t.TempDir()
	certPath := writeTestFile(t, dir, "server.crt", serverCertPEM)
	keyPath := writeTestFile(t, dir, "server.key", serverKeyPEM)
	caPath := writeTestFile(t, dir, "ca.crt", caPEM)

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(caPEM)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("plain TLS accepts a client with no certificate", func(t *testing.T) {
		cfg, ok, err := resolveTLSConfig(&runOptions{tlsCertFile: certPath, tlsKeyFile: keyPath})
		if err != nil || !ok {
			t.Fatalf("resolveTLSConfig: ok %v, err %v", ok, err)
		}
		ts := httptest.NewUnstartedServer(handler)
		ts.TLS = cfg
		ts.StartTLS()
		defer ts.Close()

		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: rootPool}}}
		resp, err := client.Get(ts.URL)
		if err != nil {
			t.Fatalf("GET without a client certificate: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("mTLS rejects a client with no certificate and accepts one signed by --tls-client-ca", func(t *testing.T) {
		cfg, ok, err := resolveTLSConfig(&runOptions{tlsCertFile: certPath, tlsKeyFile: keyPath, tlsClientCAFile: caPath})
		if err != nil || !ok {
			t.Fatalf("resolveTLSConfig: ok %v, err %v", ok, err)
		}
		ts := httptest.NewUnstartedServer(handler)
		ts.TLS = cfg
		ts.StartTLS()
		defer ts.Close()

		noCertClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: rootPool}}}
		if _, err := noCertClient.Get(ts.URL); err == nil {
			t.Fatal("GET with no client certificate: want an error, got none")
		}

		clientCert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
		if err != nil {
			t.Fatalf("loading the test client certificate: %v", err)
		}
		withCertClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:      rootPool,
			Certificates: []tls.Certificate{clientCert},
		}}}
		resp, err := withCertClient.Get(ts.URL)
		if err != nil {
			t.Fatalf("GET with a certificate signed by --tls-client-ca: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})
}

// --- TLS test fixtures --------------------------------------------------
//
// Everything below builds throwaway certificates in memory (ECDSA P-256,
// one hour of validity) for the tests above; none of it is production code.

// randomSerial returns a serial number suitable for a test certificate.
func randomSerial(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generating a serial number: %v", err)
	}
	return n
}

// newTestCA returns a self-signed CA certificate (parsed) and its key, used
// to sign the leaf certificates the handshake test presents to each side.
func newTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          randomSerial(t),
		Subject:               pkix.Name{CommonName: "forsight-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating the test CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing the test CA certificate: %v", err)
	}
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// newSignedCert issues a leaf certificate for cn, with ips as its
// SubjectAltName entries (so a client can validate it against a hostname),
// signed by ca/caKey with the given extended key usage.
func newSignedCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, ips []net.IP, eku x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a leaf key for %q: %v", cn, err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: randomSerial(t),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{eku},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating a leaf certificate for %q: %v", cn, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), marshalECKey(t, key)
}

// newSelfSignedCert is newSignedCert with the certificate as its own
// issuer — enough for tls.LoadX509KeyPair, which only checks that the key
// matches the certificate, not that the certificate chains to a trusted
// root.
func newSelfSignedCert(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key for %q: %v", cn, err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: randomSerial(t),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a self-signed certificate for %q: %v", cn, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), marshalECKey(t, key)
}

func marshalECKey(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling a private key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

// writeTestFile writes contents to name under dir and returns the path.
func writeTestFile(t *testing.T, dir, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}
