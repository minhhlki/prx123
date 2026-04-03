package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type IPAPI struct {
	Status  string `json:"status"`
	Country string `json:"country"`
	Isp     string `json:"isp"`
	Query   string `json:"query"`
}

type ProxyPrinter struct {
	enabled     bool
	displayInfo bool
	client      *http.Client
	limiter     chan struct{}
	wg          sync.WaitGroup
}

func NewProxyPrinter(cfg Config) *ProxyPrinter {
	if !cfg.PrintIps.Enabled {
		return &ProxyPrinter{}
	}

	return &ProxyPrinter{
		enabled:     true,
		displayInfo: cfg.PrintIps.DisplayIpInfo,
		client: &http.Client{
			Timeout: time.Second * time.Duration(cfg.PrintIps.Timeout),
		},
		limiter: make(chan struct{}, cfg.PrintIps.MaxConcurrent),
	}
}

func (p *ProxyPrinter) Submit(proxy string, port int) {
	if !p.enabled {
		return
	}

	if !p.displayInfo {
		fmt.Printf("\033[32mNew Proxy %s:%d\033[39m\n", proxy, port)
		return
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.limiter <- struct{}{}
		defer func() { <-p.limiter }()

		ipInfo := p.getISP(proxy)
		if ipInfo == nil {
			fmt.Printf("New Proxy \033[32m%s:%d\033[39m Country: \033[34merror\033[39m ISP: \033[34merror\033[39m\n", proxy, port)
			return
		}

		fmt.Printf("New Proxy \033[32m%s:%d\033[39m Country: \033[34m%s\033[39m ISP: \033[34m%s\033[39m\n", proxy, port, ipInfo.Country, ipInfo.Isp)
	}()
}

func (p *ProxyPrinter) Wait() {
	p.wg.Wait()
}

func (p *ProxyPrinter) getISP(proxy string) *IPAPI {
	if p.client == nil {
		return nil
	}

	res, err := p.client.Get("http://ip-api.com/json/" + proxy)
	if err != nil {
		log.Printf("ip info lookup failed for %s: %v", proxy, err)
		return nil
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if err != nil {
		log.Printf("ip info read failed for %s: %v", proxy, err)
		return nil
	}

	var ipInfo IPAPI
	if err := json.Unmarshal(body, &ipInfo); err != nil {
		log.Printf("ip info parse failed for %s: %v", proxy, err)
		return nil
	}

	return &ipInfo
}
