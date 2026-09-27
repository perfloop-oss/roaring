package roaring

import (
	"bytes"
	"testing"
)

// The portable serializer is the reference encoding, so the test binary
// serializes through it by default.
func init() { serializeViaCopy = true }

func TestPortableSerializerMatchesWriteTo(t *testing.T) {
	for _, rb := range []*Bitmap{
		BitmapOf(1, 2, 3, 1000, 1<<20),
		func() *Bitmap { b := New(); b.AddRange(0, 1<<18); b.RunOptimize(); return b }(),
		func() *Bitmap {
			b := New()
			for i := uint32(0); i < 1<<18; i += 3 {
				b.Add(i)
			}
			return b
		}(),
	} {
		var portable, direct bytes.Buffer
		if _, err := rb.WriteTo(&portable); err != nil {
			t.Fatal(err)
		}
		serializeViaCopy = false
		_, err := rb.WriteTo(&direct)
		serializeViaCopy = true
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(portable.Bytes(), direct.Bytes()) {
			t.Fatalf("portable and direct serializers differ for %v", rb)
		}
	}
}
