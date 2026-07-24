package roaring

import (
	"sync/atomic"
	"testing"
)

func benchmarkParOrHighFanInBitmapAtParallelism(b *testing.B, parallelism int) {
	inputs, expected := newParOrHighFanInBitmapInputs()
	assertBitmapOnlyParOrInputs(b, inputs)
	assertParOrResult(b, ParOr(parallelism, inputs...), expected)

	b.ReportAllocs()
	var total uint64
	for b.Loop() {
		result := ParOr(parallelism, inputs...)
		total += result.GetCardinality()
	}
	if total != expected.GetCardinality()*uint64(b.N) {
		b.Fatalf("got total cardinality %d, want %d", total, expected.GetCardinality()*uint64(b.N))
	}
}

func BenchmarkParOrHighFanInBitmapP1(b *testing.B) {
	benchmarkParOrHighFanInBitmapAtParallelism(b, 1)
}

func BenchmarkParOrHighFanInBitmapP2(b *testing.B) {
	benchmarkParOrHighFanInBitmapAtParallelism(b, 2)
}

func BenchmarkParOrHighFanInBitmapSaturated(b *testing.B) {
	inputs, expected := newParOrHighFanInBitmapInputs()
	assertBitmapOnlyParOrInputs(b, inputs)
	assertParOrResult(b, ParOr(parOrHighFanInParallelism, inputs...), expected)

	b.ReportAllocs()
	var total atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var local uint64
		for pb.Next() {
			local += ParOr(parOrHighFanInParallelism, inputs...).GetCardinality()
		}
		total.Add(local)
	})
	if total.Load() != expected.GetCardinality()*uint64(b.N) {
		b.Fatalf("got total cardinality %d, want %d", total.Load(), expected.GetCardinality()*uint64(b.N))
	}
}
