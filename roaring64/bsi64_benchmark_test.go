package roaring64

import (
	"testing"
)

func setupMediumBSI(b *testing.B) *BSI {
	bsi := NewDefaultBSI()
	for i := uint64(0); i < 50000; i++ {
		bsi.SetValue(i, int64(i%100))
	}
	return bsi
}

func BenchmarkBatchEqualMedium(b *testing.B) {
	bsi := setupMediumBSI(b)
	vals := []int64{16, 55, 57}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := bsi.BatchEqual(0, vals)
		_ = res
	}
}

func BenchmarkTransposeMedium(b *testing.B) {
	bsi := setupMediumBSI(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := bsi.Transpose()
		_ = res
	}
}
