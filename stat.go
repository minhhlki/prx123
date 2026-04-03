/*
	(c) Yariya
*/

package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

type Stats struct {
	imported      uint64
	checked       uint64
	success       uint64
	statusCodeErr uint64
	bodyErr       uint64
	proxyErr      uint64
	timeoutErr    uint64
	reported      uint64
	reportErr     uint64
	activeWorkers int64
}

func (s *Stats) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Printf(
				"Imported [\u001B[34m%d\u001B[39m] Checked [\u001B[34m%d\u001B[39m] Success [\033[32m%d\033[39m] StatusErr [\u001B[31m%d\u001B[39m] BodyErr [\u001B[31m%d\u001B[39m] ProxyErr [\u001B[31m%d\u001B[39m] Timeout [\u001B[31m%d\u001B[39m] Reported [\u001B[34m%d\u001B[39m] ActiveWorkers [\u001B[34m%d\u001B[39m]\n",
				atomic.LoadUint64(&s.imported),
				atomic.LoadUint64(&s.checked),
				atomic.LoadUint64(&s.success),
				atomic.LoadUint64(&s.statusCodeErr),
				atomic.LoadUint64(&s.bodyErr),
				atomic.LoadUint64(&s.proxyErr),
				atomic.LoadUint64(&s.timeoutErr),
				atomic.LoadUint64(&s.reported),
				atomic.LoadInt64(&s.activeWorkers),
			)
		}
	}
}
