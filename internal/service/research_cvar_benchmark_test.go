package service

import (
	"fmt"
	"math"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

const (
	researchCVaRBenchmarkAssets     = 10
	researchCVaRBenchmarkCandidates = 2000
	researchCVaRBenchmarkReturns    = 2520
)

func benchmarkResearchInput() BacktestInput {
	start := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	in := BacktestInput{BaseCurrency: "CNY", RebalancePolicy: ResearchRebalanceMonthly}
	for asset := 0; asset < researchCVaRBenchmarkAssets; asset++ {
		points := make([]ResearchSeriesPoint, researchCVaRBenchmarkReturns+1)
		value := 100 + float64(asset)
		for day := range points {
			if day > 0 {
				value *= 1 + 0.0001 + 0.0005*math.Sin(float64(day+asset*7)/31)
			}
			points[day] = ResearchSeriesPoint{
				Date: start.AddDate(0, 0, day).Format("2006-01-02"), Value: value,
			}
		}
		in.Assets = append(in.Assets, BacktestAssetInput{
			AssetKey: fmt.Sprintf("BENCH_%02d", asset), Name: fmt.Sprintf("Asset %d", asset),
			Currency: "CNY", Weight: 1.0 / researchCVaRBenchmarkAssets, Points: points,
		})
	}
	return in
}

func BenchmarkResearchOptimizationCVaR(b *testing.B) {
	for _, tc := range []struct {
		name     string
		tailRisk *TailRiskSpec
	}{
		{name: "without_tail_risk"},
		{name: "with_tail_risk", tailRisk: &TailRiskSpec{Confidence: 0.95, HorizonDays: 20}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			in := benchmarkResearchInput()
			in.TailRisk = tc.tailRisk
			runtime.GC()
			var baseline runtime.MemStats
			runtime.ReadMemStats(&baseline)
			stopSampling := make(chan struct{})
			var peakHeap atomic.Uint64
			samplingDone := make(chan struct{})
			go sampleBenchmarkPeakHeap(stopSampling, samplingDone, &peakHeap)
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				for candidate := 0; candidate < researchCVaRBenchmarkCandidates; candidate++ {
					if _, err := RunResearchBacktest(in); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.StopTimer()
			close(stopSampling)
			<-samplingDone
			peak := peakHeap.Load()
			if peak > baseline.HeapInuse {
				b.ReportMetric(float64(peak-baseline.HeapInuse)/float64(b.N), "peak_heap_B/op")
			}
		})
	}
}

func sampleBenchmarkPeakHeap(stop <-chan struct{}, done chan<- struct{}, peak *atomic.Uint64) {
	defer close(done)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			for current := peak.Load(); stats.HeapInuse > current; current = peak.Load() {
				if peak.CompareAndSwap(current, stats.HeapInuse) {
					break
				}
			}
		}
	}
}
