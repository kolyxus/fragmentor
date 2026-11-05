package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sampleEvery is how often the sampler reads the memory of the runtime.
const sampleEvery = 10 * time.Millisecond

// sampler follows the peak memory of the process: what the Go runtime holds
// from the OS and has not given back, sampled every 10 ms, and on Linux the
// peak resident set, VmHWM, that the kernel keeps itself.
type sampler struct {
	mu  sync.Mutex
	max uint64
}

func startSampler() *sampler {
	s := &sampler{}
	go s.loop()
	return s
}

func (s *sampler) loop() {
	samples := []metrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	for {
		metrics.Read(samples)
		used := samples[0].Value.Uint64() - samples[1].Value.Uint64()
		s.mu.Lock()
		s.max = max(s.max, used)
		s.mu.Unlock()
		time.Sleep(sampleEvery)
	}
}

// reset starts a new peak: it returns to the OS what the last demo left and
// resets the kernel's peak too.
func (s *sampler) reset() {
	runtime.GC()
	debug.FreeOSMemory()
	s.mu.Lock()
	s.max = 0
	s.mu.Unlock()
	// Writing 5 to clear_refs resets VmHWM, Linux only; elsewhere it fails.
	_ = os.WriteFile("/proc/self/clear_refs", []byte("5"), 0)
}

// peak describes the peak since the last reset.
func (s *sampler) peak() string {
	s.mu.Lock()
	m := s.max
	s.mu.Unlock()
	out := fmt.Sprintf("Go %d MiB", m>>20)
	if rss, ok := peakRSS(); ok {
		out += fmt.Sprintf(", RSS %d MiB", rss>>20)
	}
	return out
}

// peakRSS reads VmHWM from /proc/self/status, the peak resident set of the
// process on Linux.
func peakRSS() (uint64, bool) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "VmHWM:")
		if !ok {
			continue
		}
		kb, err := strconv.ParseUint(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "kB")), 10, 64)
		if err != nil {
			return 0, false
		}
		return kb << 10, true
	}
	return 0, false
}
