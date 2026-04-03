/*
	(c) Yariya
*/

package main

import (
	"bufio"
	"log"
	"os"
	"sync"
)

type Exporter struct {
	f      *os.File
	writer *bufio.Writer
	lines  chan string
	wg     sync.WaitGroup
	once   sync.Once
}

func NewExporter(path string, deduplicate bool) (*Exporter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o666)
	if err != nil {
		return nil, err
	}

	exporter := &Exporter{
		f:      f,
		writer: bufio.NewWriterSize(f, 64*1024),
		lines:  make(chan string, 4096),
	}

	exporter.wg.Add(1)
	go exporter.run(deduplicate)

	return exporter, nil
}

func (e *Exporter) run(deduplicate bool) {
	defer e.wg.Done()

	var seen map[string]struct{}
	if deduplicate {
		seen = make(map[string]struct{})
	}

	for line := range e.lines {
		if seen != nil {
			if _, exists := seen[line]; exists {
				continue
			}
			seen[line] = struct{}{}
		}

		if _, err := e.writer.WriteString(line + "\n"); err != nil {
			log.Printf("output write error: %v", err)
			continue
		}
	}

	if err := e.writer.Flush(); err != nil {
		log.Printf("output flush error: %v", err)
	}
	if err := e.f.Close(); err != nil {
		log.Printf("output close error: %v", err)
	}
}

func (e *Exporter) Add(s string) {
	e.lines <- s
}

func (e *Exporter) Close() {
	e.once.Do(func() {
		close(e.lines)
		e.wg.Wait()
	})
}
