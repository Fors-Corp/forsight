// Package probe watches a fixed list of HTTP(S) URLs on the collector's
// shared interval — a synthetic check, unlike every other collector in this
// tree, which observes something already running on the host. Its purpose is
// narrow but concrete: feed the dashboard's UptimeBar and give advance
// warning of a certificate about to expire, the same two questions
// blackbox_exporter answers, folded into this one binary so there's nothing
// extra to run.
//
// Reachability and certificate trust are reported as two independent
// signals. A GET always completes (or times out) regardless of whether the
// server's certificate verifies — that's deliberate: the day a cert expires
// is exactly the day an operator most wants "is the site still up" to keep
// working, distinct from "is the cert still good". So the TLS handshake
// itself never aborts on a bad chain; it's verified by hand afterward
// (verifyChain below) purely to produce probe.tls.valid.
package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// maxProbeBodyBytes bounds how much of a probed response body is read before
// it's discarded. The content itself is never used — only that the response
// completed — so there's no reason to buffer more than enough to let the
// connection wind down cleanly.
const maxProbeBodyBytes = 1 << 20 // 1 MiB

// Target is one URL to probe on the collector's shared interval.
type Target struct {
	// URL to GET, e.g. "https://example.com/healthz".
	URL string
	// Name labels every metric from this target (e.g. "checkout"). Defaults
	// to the URL's host when not given explicitly — see cmd/run.go's
	// parseProbeTargets, which mirrors --scrape's job= convention.
	Name string
}

// Collector probes a fixed set of HTTP(S) URLs.
type Collector struct {
	targets []Target
	timeout time.Duration

	// rootCAs overrides the trust store used to judge probe.tls.valid.
	// nil (the production default) means the system pool; tests set this
	// directly to trust a self-signed httptest certificate.
	rootCAs *x509.CertPool

	// warned tracks which target URLs have already logged a "never
	// resolves" warning, so a persistently bad DNS name doesn't spam the
	// log on every tick. Collect runs on a single goroutine per collector
	// instance (see collector.Registry.runOne), so this needs no lock.
	warned map[string]bool
}

// New builds a Collector for the given targets. interval is the registry's
// collect interval; each probe's own timeout is capped at 10s so one slow or
// hung target can never block past the next tick's peers by more than that.
func New(targets []Target, interval time.Duration) *Collector {
	timeout := interval
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	return &Collector{targets: targets, timeout: timeout, warned: make(map[string]bool)}
}

func (c *Collector) Name() string { return "probe" }

func (c *Collector) Collect(ctx context.Context) ([]model.Metric, error) {
	var out []model.Metric
	for _, t := range c.targets {
		out = append(out, c.probeOne(ctx, t)...)
	}
	return out, nil
}

// probeOne always returns at least a probe.http.up point — a probe failure
// (DNS, connection refused, timeout, a non-2xx status) is data, never an
// error that would make the collector skip the tick for every other target.
func (c *Collector) probeOne(ctx context.Context, t Target) []model.Metric {
	now := time.Now()
	labels := map[string]string{"url": t.URL, "name": t.Name}

	u, err := url.Parse(t.URL)
	if err != nil {
		// Validated already by cmd/run.go's parseProbeTargets before this
		// collector is ever constructed; kept defensive rather than assumed.
		return []model.Metric{{Name: "probe.http.up", Value: 0, Timestamp: now, Labels: labels}}
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, t.URL, nil)
	if err != nil {
		return []model.Metric{{Name: "probe.http.up", Value: 0, Timestamp: now, Labels: labels}}
	}

	var cert tlsCapture
	client := &http.Client{
		Timeout:   c.timeout,
		Transport: &http.Transport{TLSClientConfig: c.tlsConfig(u.Hostname(), &cert)},
		// CheckRedirect left nil: default net/http behaviour (follow up to
		// 10), per the spec's "no redirects beyond default client behaviour".
	}

	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)

	var metrics []model.Metric
	if err != nil {
		c.logIfUnresolvable(t.URL, err)
		metrics = append(metrics,
			model.Metric{Name: "probe.http.up", Value: 0, Timestamp: now, Labels: labels},
			model.Metric{Name: "probe.http.duration_ms", Value: float64(duration.Milliseconds()), Timestamp: now, Labels: labels},
		)
	} else {
		func() {
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.CopyN(io.Discard, resp.Body, maxProbeBodyBytes)
		}()
		metrics = append(metrics,
			model.Metric{Name: "probe.http.up", Value: 1, Timestamp: now, Labels: labels},
			model.Metric{Name: "probe.http.status", Value: float64(resp.StatusCode), Timestamp: now, Labels: labels},
			model.Metric{Name: "probe.http.duration_ms", Value: float64(duration.Milliseconds()), Timestamp: now, Labels: labels},
		)
	}

	if u.Scheme == "https" && cert.leaf != nil {
		validValue := 0.0
		if cert.valid {
			validValue = 1
		}
		daysRemaining := time.Until(cert.leaf.NotAfter).Hours() / 24
		metrics = append(metrics,
			model.Metric{Name: "probe.tls.days_remaining", Value: daysRemaining, Timestamp: now, Labels: labels},
			model.Metric{Name: "probe.tls.valid", Value: validValue, Timestamp: now, Labels: labels},
		)
	}

	return metrics
}

// tlsCapture holds what the manual chain check below found, read back by
// probeOne once the request has completed.
type tlsCapture struct {
	leaf  *x509.Certificate
	valid bool
}

// tlsConfig builds a per-request TLS config whose VerifyConnection hook
// records the leaf certificate and a from-scratch chain verification,
// without ever failing the handshake itself — see the package doc for why.
// A fresh config (not a shared one) per request is what makes capturing the
// result into cert race-free without a lock: each probe gets its own
// closure over its own tlsCapture.
func (c *Collector) tlsConfig(hostname string, cert *tlsCapture) *tls.Config {
	return &tls.Config{
		// Go's own verification is skipped so an expired or untrusted
		// certificate never aborts the handshake; verifyChain below runs the
		// same check by hand purely to report probe.tls.valid.
		InsecureSkipVerify: true, //nolint:gosec // verified manually in VerifyConnection
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return nil
			}
			cert.leaf = state.PeerCertificates[0]
			cert.valid = c.verifyChain(hostname, state.PeerCertificates)
			return nil
		},
	}
}

// verifyChain runs the same check crypto/tls would have run had
// InsecureSkipVerify been false, against c.rootCAs (nil meaning the system
// pool, exactly like x509.VerifyOptions{Roots: nil}).
func (c *Collector) verifyChain(hostname string, peerCerts []*x509.Certificate) bool {
	leaf := peerCerts[0]
	intermediates := x509.NewCertPool()
	for _, cert := range peerCerts[1:] {
		intermediates.AddCert(cert)
	}
	_, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       hostname,
		Roots:         c.rootCAs,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err == nil
}

// logIfUnresolvable warns once per target URL the first time its hostname
// fails to resolve at all — a typo'd or decommissioned URL, worth an
// operator's attention — rather than on every tick, which for a collector
// polling every few seconds would flood the log for as long as the URL stays
// configured. A timeout or refused connection is not logged here: those show
// up as probe.http.up=0 every tick already, which is the whole point of the
// metric.
func (c *Collector) logIfUnresolvable(rawURL string, err error) {
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
		return
	}
	if c.warned[rawURL] {
		return
	}
	c.warned[rawURL] = true
	slog.Default().Warn("probe target does not resolve", "url", rawURL, "error", err)
}
