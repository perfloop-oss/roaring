package roaring

import (
	"testing"
)

func BenchmarkTranspose32(b *testing.B) {
	bsi := setupLargeBSI(b)
	if bsi == nil {
		b.Skip("skipping, large BSI setup failed")
		return
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := bsi.Transpose()
		_ = res
	}
}
