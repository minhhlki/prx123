package main

import (
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type ProxyReporter struct {
	enabled bool
	url     string
	client  *http.Client
	limiter chan struct{}
	wg      sync.WaitGroup
	stats   *Stats
}

func NewProxyReporter(cfg Config, stats *Stats) *ProxyReporter {
	if !cfg.ReportProxy.Enabled {
		return &ProxyReporter{}
	}

	return &ProxyReporter{
		enabled: true,
		url:     cfg.ReportProxy.URL,
		client: &http.Client{
			Timeout: time.Second * time.Duration(cfg.ReportProxy.Timeout),
		},
		limiter: make(chan struct{}, cfg.ReportProxy.MaxConcurrent),
		stats:   stats,
	}
}

func (r *ProxyReporter) Submit(proxyString string) {
	if !r.enabled || r.client == nil || r.url == "" {
		return
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.limiter <- struct{}{}
		defer func() { <-r.limiter }()

		data := url.Values{}
		data.Set("proxy", proxyString)

		resp, err := r.client.PostForm(r.url, data)
		if err != nil {
			atomic.AddUint64(&r.stats.reportErr, 1)
			log.Printf("failed to report proxy %s: %v", proxyString, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			atomic.AddUint64(&r.stats.reportErr, 1)
			log.Printf("report endpoint returned status %d for %s", resp.StatusCode, proxyString)
			return
		}

		atomic.AddUint64(&r.stats.reported, 1)
	}()
}

func (r *ProxyReporter) Wait() {
	r.wg.Wait()
}
