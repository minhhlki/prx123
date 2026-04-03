/*
   (c) Yariya
*/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
)

var port = flag.Int("p", 80, "proxy port")
var output = flag.String("o", "output.txt", "output file")
var configFile = flag.String("cfg", "config.json", "configuration file")

var input = flag.String("in", "", "input file to check")
var fetch = flag.String("url", "", "url proxy fetch")

type Config struct {
	CheckSite         string `json:"check-site"`
	ExpectedStatus    int    `json:"expected-status"`
	ExpectedBody      string `json:"expected-body"`
	ProxyType         string `json:"proxy-type"`
	HttpThreads       int    `json:"http_threads"`
	QueueSize         int    `json:"queue_size"`
	DeduplicateInput  bool   `json:"deduplicate_input"`
	DeduplicateOutput bool   `json:"deduplicate_output"`
	Headers           struct {
		UserAgent string `json:"user-agent"`
		Accept    string `json:"accept"`
	} `json:"headers"`
	PrintIps struct {
		Enabled       bool `json:"enabled"`
		DisplayIpInfo bool `json:"display-ip-info"`
		MaxConcurrent int  `json:"max-concurrent"`
		Timeout       int  `json:"timeout"`
	} `json:"print_ips"`
	ReportProxy struct {
		Enabled       bool   `json:"enabled"`
		URL           string `json:"url"`
		Timeout       int    `json:"timeout"`
		MaxConcurrent int    `json:"max-concurrent"`
	} `json:"report_proxy"`
	Timeout struct {
		HttpTimeout   int `json:"http_timeout"`
		Socks4Timeout int `json:"socks4_timeout"`
		Socks5Timeout int `json:"socks5_timeout"`
	} `json:"timeout"`
}

var config Config

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Zmap ProxyScanner @tcpfin\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if err := loadConfig(*configFile); err != nil {
		log.Printf("config error: %v", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	exporter, err := NewExporter(*output, config.DeduplicateOutput)
	if err != nil {
		log.Printf("exporter error: %v", err)
		os.Exit(1)
	}
	defer exporter.Close()

	stats := &Stats{}
	go stats.Run(ctx)

	printer := NewProxyPrinter(config)
	reporter := NewProxyReporter(config, stats)

	checker, err := NewChecker(config, exporter, printer, reporter, stats)
	if err != nil {
		log.Printf("checker init error: %v", err)
		os.Exit(1)
	}

	jobs := make(chan ProxyJob, config.QueueSize)
	workerDone := checker.StartWorkers(ctx, jobs)

	importErr := EnqueueProxies(ctx, jobs, stats)
	close(jobs)
	workerDone.Wait()
	reporter.Wait()
	printer.Wait()
	stop()

	if importErr != nil && !errors.Is(importErr, context.Canceled) {
		log.Printf("input error: %v", importErr)
		os.Exit(1)
	}
}

func loadConfig(path string) error {
	cfgBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(cfgBytes, &config); err != nil {
		return err
	}
	applyConfigDefaults(&config)
	return validateConfig(&config)
}

func applyConfigDefaults(cfg *Config) {
	cfg.ProxyType = strings.ToLower(strings.TrimSpace(cfg.ProxyType))
	if cfg.ProxyType == "" {
		cfg.ProxyType = "http"
	}
	if cfg.CheckSite == "" {
		cfg.CheckSite = "https://check-ec8.pages.dev/ping.txt"
	}
	if cfg.ExpectedStatus == 0 {
		cfg.ExpectedStatus = 200
	}
	if cfg.HttpThreads <= 0 {
		cfg.HttpThreads = 200
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = cfg.HttpThreads * 20
	}
	if cfg.Headers.UserAgent == "" {
		cfg.Headers.UserAgent = "Mozilla/5.0 (compatible; ZmapProxyScanner/2.0; +https://check-ec8.pages.dev/ping.txt)"
	}
	if cfg.Headers.Accept == "" {
		cfg.Headers.Accept = "text/plain,*/*;q=0.8"
	}
	if cfg.Timeout.HttpTimeout <= 0 {
		cfg.Timeout.HttpTimeout = 5
	}
	if cfg.Timeout.Socks4Timeout <= 0 {
		cfg.Timeout.Socks4Timeout = cfg.Timeout.HttpTimeout
	}
	if cfg.Timeout.Socks5Timeout <= 0 {
		cfg.Timeout.Socks5Timeout = cfg.Timeout.HttpTimeout
	}
	if cfg.PrintIps.MaxConcurrent <= 0 {
		cfg.PrintIps.MaxConcurrent = 4
	}
	if cfg.PrintIps.Timeout <= 0 {
		cfg.PrintIps.Timeout = 5
	}
	if cfg.ReportProxy.MaxConcurrent <= 0 {
		cfg.ReportProxy.MaxConcurrent = 4
	}
	if cfg.ReportProxy.Timeout <= 0 {
		cfg.ReportProxy.Timeout = 15
	}
	if cfg.ReportProxy.URL == "" {
		cfg.ReportProxy.URL = "https://shareproxy.pro/proxy.php"
	}
}

func validateConfig(cfg *Config) error {
	switch cfg.ProxyType {
	case "http", "socks4", "socks5":
	default:
		return fmt.Errorf("invalid proxy-type %q", cfg.ProxyType)
	}
	if cfg.CheckSite == "" {
		return fmt.Errorf("check-site must not be empty")
	}
	if cfg.HttpThreads <= 0 {
		return fmt.Errorf("http_threads must be greater than zero")
	}
	if cfg.QueueSize <= 0 {
		return fmt.Errorf("queue_size must be greater than zero")
	}
	return nil
}
