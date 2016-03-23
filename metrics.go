/*-
 * Copyright 2016 Square Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package sqmetrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/rcrowley/go-metrics"
)

// SquareMetrics posts metrics to an HTTP/JSON bridge endpoint
type SquareMetrics struct {
	registry metrics.Registry
	url      string
	prefix   string
	hostname string
}

// NewMetrics is the entry point for this code
func NewMetrics(metricsURL, metricsPrefix string, registry metrics.Registry) *SquareMetrics {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}

	metrics := &SquareMetrics{
		registry: registry,
		url:      metricsURL,
		prefix:   metricsPrefix,
		hostname: hostname,
	}

	if metricsURL != "" {
		go metrics.publishMetrics()
	}

	go metrics.collectSystemMetrics()
	return metrics
}

func (mb *SquareMetrics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	metrics := mb.SerializeMetrics()
	raw, err := json.Marshal(metrics)
	if err != nil {
		panic(err)
	}
	w.Write(raw)
}

// Publish metrics to bridge
func (mb *SquareMetrics) publishMetrics() {
	for range time.Tick(1 * time.Second) {
		mb.postMetrics()
	}
}

// Collect memory usage metrics
func (mb *SquareMetrics) collectSystemMetrics() {
	var mem runtime.MemStats

	alloc := metrics.GetOrRegisterGauge("runtime.mem.alloc", mb.registry)
	totalAlloc := metrics.GetOrRegisterGauge("runtime.mem.total-alloc", mb.registry)
	sys := metrics.GetOrRegisterGauge("runtime.mem.sys", mb.registry)
	heapAlloc := metrics.GetOrRegisterGauge("runtime.mem.heap.alloc", mb.registry)
	heapSys := metrics.GetOrRegisterGauge("runtime.mem.heap.sys", mb.registry)
	heapInUse := metrics.GetOrRegisterGauge("runtime.mem.heap.in-use", mb.registry)
	stackSys := metrics.GetOrRegisterGauge("runtime.mem.stack.sys", mb.registry)
	stackInUse := metrics.GetOrRegisterGauge("runtime.mem.stack.in-use", mb.registry)
	gcPauseTotal := metrics.GetOrRegisterGauge("runtime.mem.gc.pause-total", mb.registry)
	gcCPUFraction := metrics.GetOrRegisterGaugeFloat64("runtime.mem.gc.cpu-fraction", mb.registry)
	numGoRoutines := metrics.GetOrRegisterGauge("runtime.goroutines", mb.registry)
	numCgoCalls := metrics.GetOrRegisterGauge("runtime.cgo-calls", mb.registry)

	for range time.Tick(1 * time.Second) {
		runtime.ReadMemStats(&mem)

		alloc.Update(int64(mem.Alloc))
		totalAlloc.Update(int64(mem.TotalAlloc))
		sys.Update(int64(mem.Sys))

		heapAlloc.Update(int64(mem.HeapAlloc))
		heapSys.Update(int64(mem.HeapSys))
		heapInUse.Update(int64(mem.HeapInuse))

		stackSys.Update(int64(mem.StackSys))
		stackInUse.Update(int64(mem.StackInuse))

		gcPauseTotal.Update(int64(mem.PauseTotalNs))
		gcCPUFraction.Update(mem.GCCPUFraction)

		numGoRoutines.Update(int64(runtime.NumGoroutine()))
		numCgoCalls.Update(int64(runtime.NumCgoCall()))
	}
}

func (mb *SquareMetrics) postMetrics() {
	metrics := mb.SerializeMetrics()
	raw, err := json.Marshal(metrics)
	if err != nil {
		panic(err)
	}
	resp, err := http.Post(mb.url, "application/json", bytes.NewReader(raw))
	if err == nil {
		resp.Body.Close()
	}
}

func (mb *SquareMetrics) serializeMetric(now int64, metric tuple) map[string]interface{} {
	return map[string]interface{}{
		"timestamp": now,
		"metric":    fmt.Sprintf("%s.%s", mb.prefix, metric.name),
		"value":     metric.value,
		"hostname":  mb.hostname,
	}
}

type tuple struct {
	name  string
	value interface{}
}

// SerializeMetrics returns a map of the collected metrics, suitable for JSON marshalling
func (mb *SquareMetrics) SerializeMetrics() []map[string]interface{} {
	nvs := []tuple{}

	mb.registry.Each(func(name string, i interface{}) {
		switch metric := i.(type) {
		case metrics.Counter:
			nvs = append(nvs, tuple{name, metric.Count()})
		case metrics.Gauge:
			nvs = append(nvs, tuple{name, metric.Value()})
		case metrics.GaugeFloat64:
			nvs = append(nvs, tuple{name, metric.Value()})
		case metrics.Timer:
			timer := metric.Snapshot()
			nvs = append(nvs, []tuple{
				{fmt.Sprintf("%s.count", name), timer.Count()},
				{fmt.Sprintf("%s.min", name), timer.Min()},
				{fmt.Sprintf("%s.max", name), timer.Max()},
				{fmt.Sprintf("%s.mean", name), timer.Mean()},
				{fmt.Sprintf("%s.std-dev", name), timer.StdDev()},
				{fmt.Sprintf("%s.one-minute", name), timer.Rate1()},
				{fmt.Sprintf("%s.five-minute", name), timer.Rate5()},
				{fmt.Sprintf("%s.fifteen-minute", name), timer.Rate15()},
				{fmt.Sprintf("%s.mean-rate", name), timer.RateMean()},
				{fmt.Sprintf("%s.50-percentile", name), timer.Percentile(0.5)},
				{fmt.Sprintf("%s.75-percentile", name), timer.Percentile(0.75)},
				{fmt.Sprintf("%s.95-percentile", name), timer.Percentile(0.95)},
				{fmt.Sprintf("%s.99-percentile", name), timer.Percentile(0.99)},
				{fmt.Sprintf("%s.999-percentile", name), timer.Percentile(0.999)},
			}...)
		}
	})

	now := time.Now().Unix()
	out := []map[string]interface{}{}
	for _, nv := range nvs {
		out = append(out, mb.serializeMetric(now, nv))
	}

	return out
}
