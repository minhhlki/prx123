/*
	(c) Yariya
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"h12.io/socks"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ProxyJob struct {
	Host string
	Port int
	Raw  string
}

func (p ProxyJob) Address() string {
	return fmt.Sprintf("%s:%d", p.Host, p.Port)
}

type Checker struct {
	config   Config
	exporter *Exporter
	printer  *ProxyPrinter
	reporter *ProxyReporter
	stats    *Stats
	client   *http.Client
}

type proxyURLContextKey struct{}
type socksDialContextKey struct{}

var (
	errUnexpectedStatus = errors.New("unexpected status code")
	errUnexpectedBody   = errors.New("unexpected response body")
)

func NewChecker(cfg Config, exporter *Exporter, printer *ProxyPrinter, reporter *ProxyReporter, stats *Stats) (*Checker, error) {
	timeout := time.Second * time.Duration(cfg.Timeout.HttpTimeout)
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy:                 proxyURLFromContext,
		DialContext:           dialContextFromProxyContext(dialer),
		DisableKeepAlives:     true,
		DisableCompression:    true,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          0,
		MaxIdleConnsPerHost:   1,
		IdleConnTimeout:       0,
		ResponseHeaderTimeout: timeout,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: time.Second,
	}

	return &Checker{
		config:   cfg,
		exporter: exporter,
		printer:  printer,
		reporter: reporter,
		stats:    stats,
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}, nil
}

func (c *Checker) StartWorkers(ctx context.Context, jobs <-chan ProxyJob) *sync.WaitGroup {
	var wg sync.WaitGroup
	for workerID := 0; workerID < c.config.HttpThreads; workerID++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.workerLoop(ctx, jobs)
		}()
	}
	return &wg
}

func (c *Checker) workerLoop(ctx context.Context, jobs <-chan ProxyJob) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}

			atomic.AddInt64(&c.stats.activeWorkers, 1)
			c.checkProxy(ctx, job)
			atomic.AddInt64(&c.stats.activeWorkers, -1)
		}
	}
}

func (c *Checker) checkProxy(ctx context.Context, job ProxyJob) {
	defer atomic.AddUint64(&c.stats.checked, 1)

	reqCtx, err := c.attachProxyContext(ctx, job)
	if err != nil {
		atomic.AddUint64(&c.stats.proxyErr, 1)
		log.Printf("invalid proxy %q: %v", job.Raw, err)
		return
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.config.CheckSite, nil)
	if err != nil {
		atomic.AddUint64(&c.stats.proxyErr, 1)
		log.Printf("request build error: %v", err)
		return
	}
	req.Header.Set("User-Agent", c.config.Headers.UserAgent)
	req.Header.Set("Accept", c.config.Headers.Accept)

	res, err := c.client.Do(req)
	if err != nil {
		atomic.AddUint64(&c.stats.proxyErr, 1)
		if isTimeoutError(err) {
			atomic.AddUint64(&c.stats.timeoutErr, 1)
		}
		return
	}
	defer res.Body.Close()

	if res.StatusCode != c.config.ExpectedStatus {
		atomic.AddUint64(&c.stats.statusCodeErr, 1)
		return
	}

	if c.config.ExpectedBody != "" {
		body, err := io.ReadAll(io.LimitReader(res.Body, int64(len(c.config.ExpectedBody)+1024)))
		if err != nil {
			atomic.AddUint64(&c.stats.proxyErr, 1)
			return
		}
		if strings.TrimSpace(string(body)) != c.config.ExpectedBody {
			atomic.AddUint64(&c.stats.bodyErr, 1)
			return
		}
	}

	proxyString := job.Address()
	atomic.AddUint64(&c.stats.success, 1)
	c.exporter.Add(proxyString)
	c.printer.Submit(job.Host, job.Port)
	c.reporter.Submit(proxyString)
}

func (c *Checker) attachProxyContext(parent context.Context, job ProxyJob) (context.Context, error) {
	switch c.config.ProxyType {
	case "http":
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   job.Address(),
		}
		return context.WithValue(parent, proxyURLContextKey{}, proxyURL), nil
	case "socks4":
		dialFn := socks.Dial(fmt.Sprintf("socks4://%s?timeout=%ds", job.Address(), c.config.Timeout.Socks4Timeout))
		return context.WithValue(parent, socksDialContextKey{}, dialFn), nil
	case "socks5":
		dialFn := socks.Dial(fmt.Sprintf("socks5://%s?timeout=%ds", job.Address(), c.config.Timeout.Socks5Timeout))
		return context.WithValue(parent, socksDialContextKey{}, dialFn), nil
	default:
		return nil, fmt.Errorf("unsupported proxy-type %q", c.config.ProxyType)
	}
}

func ParseProxyJob(line string, defaultPort int) (ProxyJob, error) {
	raw := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if raw == "" {
		return ProxyJob{}, errors.New("empty proxy line")
	}

	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		raw = parsed.Host
	}

	host := raw
	port := defaultPort

	if h, p, err := net.SplitHostPort(raw); err == nil {
		host = h
		parsedPort, convErr := strconv.Atoi(strings.TrimSpace(p))
		if convErr != nil {
			return ProxyJob{}, fmt.Errorf("invalid port in %q: %w", raw, convErr)
		}
		port = parsedPort
	} else if idx := strings.LastIndex(raw, ":"); idx > 0 {
		candidatePort := strings.TrimSpace(raw[idx+1:])
		if parsedPort, convErr := strconv.Atoi(candidatePort); convErr == nil {
			host = strings.TrimSpace(raw[:idx])
			port = parsedPort
		}
	}

	host = strings.Trim(host, "[]")
	if host == "" {
		return ProxyJob{}, fmt.Errorf("missing host in %q", raw)
	}
	if port <= 0 || port > 65535 {
		return ProxyJob{}, fmt.Errorf("invalid port %d in %q", port, raw)
	}

	return ProxyJob{
		Host: host,
		Port: port,
		Raw:  raw,
	}, nil
}

func proxyURLFromContext(req *http.Request) (*url.URL, error) {
	if req == nil {
		return nil, nil
	}
	proxyURL, _ := req.Context().Value(proxyURLContextKey{}).(*url.URL)
	return proxyURL, nil
}

func dialContextFromProxyContext(defaultDialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if dialFn, ok := ctx.Value(socksDialContextKey{}).(func(string, string) (net.Conn, error)); ok {
			return dialFn(network, addr)
		}
		return defaultDialer.DialContext(ctx, network, addr)
	}
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
