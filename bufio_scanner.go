/*
	(c) Yariya
*/

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

func EnqueueProxies(ctx context.Context, jobs chan<- ProxyJob, stats *Stats) error {
	switch {
	case *fetch != "":
		log.Printf("Detected URL Mode.")
		client := &http.Client{
			Timeout: time.Second * time.Duration(config.Timeout.HttpTimeout),
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, *fetch, nil)
		if err != nil {
			return err
		}

		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("fetch returned status %d", res.StatusCode)
		}

		return enqueueFromReader(ctx, res.Body, jobs, stats, config.DeduplicateInput)
	case *input != "":
		fmt.Printf("Detected FILE Mode.\n")
		f, err := os.Open(*input)
		if err != nil {
			return err
		}
		defer f.Close()

		return enqueueFromReader(ctx, f, jobs, stats, config.DeduplicateInput)
	default:
		fmt.Printf("Detected ZMAP Mode.\n")
		return enqueueFromReader(ctx, os.Stdin, jobs, stats, config.DeduplicateInput)
	}
}

func enqueueFromReader(ctx context.Context, reader io.Reader, jobs chan<- ProxyJob, stats *Stats, deduplicate bool) error {
	if closer, ok := reader.(io.Closer); ok {
		done := make(chan struct{})
		defer close(done)

		go func() {
			select {
			case <-ctx.Done():
				_ = closer.Close()
			case <-done:
			}
		}()
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var seen map[string]struct{}
	if deduplicate {
		seen = make(map[string]struct{})
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		job, err := ParseProxyJob(scanner.Text(), *port)
		if err != nil {
			continue
		}

		if deduplicate {
			if _, exists := seen[job.Address()]; exists {
				continue
			}
			seen[job.Address()] = struct{}{}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- job:
			atomic.AddUint64(&stats.imported, 1)
		}
	}

	return scanner.Err()
}
