package roaring

import (
	"encoding/binary"
	"math/rand"
	"runtime"
	"testing"
)

func BenchmarkIntersectionLargeParallel(b *testing.B) {
	b.StopTimer()

	initsize := 650000
	r := rand.New(rand.NewSource(0))

	s1 := NewBitmap()
	sz := 150 * 1000 * 1000
	for i := 0; i < initsize; i++ {
		s1.Add(uint32(r.Int31n(int32(sz))))
	}

	s2 := NewBitmap()
	sz = 100 * 1000 * 1000
	for i := 0; i < initsize; i++ {
		s2.Add(uint32(r.Int31n(int32(sz))))
	}

	b.StartTimer()
	card := uint64(0)
	for j := 0; j < b.N; j++ {
		s3 := ParAnd(0, s1, s2)
		card = card + s3.GetCardinality()
	}
}

func BenchmarkIntersectionLargeRoaring(b *testing.B) {
	b.StopTimer()
	initsize := 650000
	r := rand.New(rand.NewSource(0))

	s1 := NewBitmap()
	sz := 150 * 1000 * 1000
	for i := 0; i < initsize; i++ {
		s1.Add(uint32(r.Int31n(int32(sz))))
	}

	s2 := NewBitmap()
	sz = 100 * 1000 * 1000
	for i := 0; i < initsize; i++ {
		s2.Add(uint32(r.Int31n(int32(sz))))
	}

	b.StartTimer()
	card := uint64(0)
	for j := 0; j < b.N; j++ {
		s3 := And(s1, s2)
		card = card + s3.GetCardinality()
	}
}

const multiwayBitmapOrTestInputCount = 512

func newSerializedOneKeyBitmaps(t *testing.T, key uint16) [][]byte {
	t.Helper()

	serialized := make([][]byte, 4)
	for phase := uint32(0); phase < uint32(len(serialized)); phase++ {
		bitmap := NewBitmap()
		for low := phase * 2; low <= MaxUint16; low += 8 {
			bitmap.Add(uint32(key)<<16 | low)
		}
		if _, ok := bitmap.highlowcontainer.containers[0].(*bitmapContainer); !ok {
			t.Fatal("test input is not bitmap-backed")
		}
		var err error
		serialized[phase], err = bitmap.ToBytes()
		if err != nil {
			t.Fatalf("serialize bitmap: %v", err)
		}
	}
	return serialized
}

// sparseBitmapBackedSerializedInput returns a serialized bitmap whose header
// declares a bitmap container while the retained bitmap words contain two bits.
func sparseBitmapBackedSerializedInput(t *testing.T, key uint16) []byte {
	t.Helper()

	source := NewBitmap()
	for i := 0; i <= arrayDefaultMaxSize; i++ {
		source.Add(uint32(key)<<16 | uint32(i*15))
	}
	if _, ok := source.highlowcontainer.containers[0].(*bitmapContainer); !ok {
		t.Fatal("source is not bitmap-backed")
	}
	serialized, err := source.ToBytes()
	if err != nil {
		t.Fatalf("serialize source: %v", err)
	}

	const bitmapOffset = 16 // no-run header, one key/card pair, and one offset
	if len(serialized) != bitmapOffset+bitmapContainerSize*8 {
		t.Fatalf("serialized bitmap is %d bytes, want %d", len(serialized), bitmapOffset+bitmapContainerSize*8)
	}
	// The portable format stores cardinality - 1 at bytes 10:12. Keep it above
	// the array threshold so FromBuffer selects a bitmapContainer, then replace
	// its retained word plane with a sparse one.
	binary.LittleEndian.PutUint16(serialized[10:12], uint16(arrayDefaultMaxSize))
	clear(serialized[bitmapOffset:])
	serialized[bitmapOffset] = 0x03
	return serialized
}

// duplicateKeyBitmapSerializedInput returns two bitmap containers with the
// same key and disjoint word planes through the portable serialized format.
func duplicateKeyBitmapSerializedInput(t *testing.T, key uint16) []byte {
	t.Helper()

	source := NewBitmap()
	for low := uint32(0); low < 1<<16; low += 2 {
		source.Add(uint32(key)<<16 | low)
	}
	for low := uint32(1); low < 1<<16; low += 2 {
		source.Add(uint32(key+1)<<16 | low)
	}
	if source.highlowcontainer.size() != 2 {
		t.Fatalf("source has %d containers, want 2", source.highlowcontainer.size())
	}
	for i, container := range source.highlowcontainer.containers {
		if _, ok := container.(*bitmapContainer); !ok {
			t.Fatalf("source container %d is %T, want *bitmapContainer", i, container)
		}
	}
	serialized, err := source.ToBytes()
	if err != nil {
		t.Fatalf("serialize source: %v", err)
	}

	const headerSize = 24 // no-run header, two key/card pairs, and two offsets
	if len(serialized) != headerSize+2*bitmapContainerSize*8 {
		t.Fatalf("serialized bitmap is %d bytes, want %d", len(serialized), headerSize+2*bitmapContainerSize*8)
	}
	// The second key is the third uint16 in the descriptive header.
	binary.LittleEndian.PutUint16(serialized[12:14], key)
	return serialized
}

func loadSerializedOneKeyBitmaps(t *testing.T, serialized [][]byte, count int) []*Bitmap {
	t.Helper()

	bitmaps := make([]*Bitmap, count)
	for i := range bitmaps {
		bitmaps[i] = NewBitmap()
		if _, err := bitmaps[i].FromBuffer(serialized[i%len(serialized)]); err != nil {
			t.Fatalf("deserialize bitmap %d: %v", i, err)
		}
		if bitmaps[i].highlowcontainer.size() != 1 {
			t.Fatalf("bitmap %d has %d containers, want 1", i, bitmaps[i].highlowcontainer.size())
		}
		if _, ok := bitmaps[i].highlowcontainer.containers[0].(*bitmapContainer); !ok {
			t.Fatalf("bitmap %d is not bitmap-backed", i)
		}
		if !bitmaps[i].highlowcontainer.needCopyOnWrite[0] {
			t.Fatalf("bitmap %d is not copy-on-write after FromBuffer", i)
		}
	}
	return bitmaps
}

func TestParOrMultiwayBitmapOrFromBuffer(t *testing.T) {
	if multiwayBitmapOrTestInputCount != multiwayBitmapOrInputCount {
		t.Fatalf("test uses %d inputs, fast path starts at %d", multiwayBitmapOrTestInputCount, multiwayBitmapOrInputCount)
	}

	const key = uint16(7)
	serialized := newSerializedOneKeyBitmaps(t, key)
	parallelInputs := loadSerializedOneKeyBitmaps(t, serialized, multiwayBitmapOrTestInputCount)
	fastInputs := loadSerializedOneKeyBitmaps(t, serialized, multiwayBitmapOrTestInputCount)
	inputSnapshots := make([]*Bitmap, len(parallelInputs))
	for i, input := range parallelInputs {
		inputSnapshots[i] = input.Clone()
	}

	want := FastOr(fastInputs...)
	got := ParOr(4, parallelInputs...)
	if !got.Equals(want) {
		t.Fatal("ParOr result differs from FastOr")
	}
	if got.GetCardinality() != want.GetCardinality() {
		t.Fatalf("ParOr cardinality %d, want %d", got.GetCardinality(), want.GetCardinality())
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("validate ParOr result: %v", err)
	}
	if err := want.Validate(); err != nil {
		t.Fatalf("validate FastOr result: %v", err)
	}

	for i, input := range parallelInputs {
		if !input.Equals(inputSnapshots[i]) {
			t.Fatalf("ParOr modified input %d", i)
		}
		if err := input.Validate(); err != nil {
			t.Fatalf("validate input %d after ParOr: %v", i, err)
		}
	}

	mutation := uint32(key)<<16 | 1
	if got.Contains(mutation) {
		t.Fatalf("test mutation value %d is already present", mutation)
	}
	got.Add(mutation)
	if !got.Contains(mutation) {
		t.Fatalf("ParOr result did not retain mutation %d", mutation)
	}
	for i, input := range parallelInputs {
		if input.Contains(mutation) {
			t.Fatalf("mutating ParOr result changed imported input %d", i)
		}
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("validate mutated ParOr result: %v", err)
	}
}

func TestParOrMultiwayBitmapOrDuplicateImportedKeyFallsBack(t *testing.T) {
	if multiwayBitmapOrTestInputCount != multiwayBitmapOrInputCount {
		t.Fatalf("test uses %d inputs, fast path starts at %d", multiwayBitmapOrTestInputCount, multiwayBitmapOrInputCount)
	}

	const key = uint16(12)
	serialized := duplicateKeyBitmapSerializedInput(t, key)
	loadInputs := func() []*Bitmap {
		inputs := make([]*Bitmap, multiwayBitmapOrTestInputCount)
		for i := range inputs {
			inputs[i] = NewBitmap()
			if _, err := inputs[i].FromBuffer(append([]byte(nil), serialized...)); err != nil {
				t.Fatalf("deserialize bitmap %d: %v", i, err)
			}
			if inputs[i].highlowcontainer.size() != 2 {
				t.Fatalf("bitmap %d has %d containers, want 2", i, inputs[i].highlowcontainer.size())
			}
			for containerIndex, container := range inputs[i].highlowcontainer.containers {
				if _, ok := container.(*bitmapContainer); !ok {
					t.Fatalf("bitmap %d container %d is %T, want *bitmapContainer", i, containerIndex, container)
				}
			}
			if inputs[i].highlowcontainer.keys[0] != key || inputs[i].highlowcontainer.keys[1] != key {
				t.Fatalf("bitmap %d keys are %v, want duplicate key %d", i, inputs[i].highlowcontainer.keys, key)
			}
		}
		return inputs
	}

	parallelInputs := loadInputs()
	inputSnapshots := make([]*Bitmap, len(parallelInputs))
	for i, input := range parallelInputs {
		inputSnapshots[i] = input.Clone()
	}
	want := FastOr(loadInputs()...)
	got := ParOr(4, parallelInputs...)
	if !got.Equals(want) {
		t.Fatal("ParOr result differs from FastOr")
	}
	if got.GetCardinality() != want.GetCardinality() {
		t.Fatalf("ParOr cardinality %d, want %d", got.GetCardinality(), want.GetCardinality())
	}
	for i, input := range parallelInputs {
		if !input.Equals(inputSnapshots[i]) {
			t.Fatalf("ParOr modified imported input %d", i)
		}
	}
}

func TestParOrMultiwayBitmapOrSparseImportedHeader(t *testing.T) {
	if multiwayBitmapOrTestInputCount != multiwayBitmapOrInputCount {
		t.Fatalf("test uses %d inputs, fast path starts at %d", multiwayBitmapOrTestInputCount, multiwayBitmapOrInputCount)
	}

	const key = uint16(8)
	serialized := sparseBitmapBackedSerializedInput(t, key)
	loadInputs := func() []*Bitmap {
		inputs := make([]*Bitmap, multiwayBitmapOrTestInputCount)
		for i := range inputs {
			inputs[i] = NewBitmap()
			if _, err := inputs[i].FromBuffer(append([]byte(nil), serialized...)); err != nil {
				t.Fatalf("deserialize bitmap %d: %v", i, err)
			}
			if _, ok := inputs[i].highlowcontainer.containers[0].(*bitmapContainer); !ok {
				t.Fatalf("bitmap %d is not bitmap-backed", i)
			}
		}
		return inputs
	}

	want := FastOr(loadInputs()...)
	got := ParOr(4, loadInputs()...)
	if !got.Equals(want) {
		t.Fatal("ParOr result differs from FastOr")
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("validate ParOr result: %v", err)
	}
	if err := want.Validate(); err != nil {
		t.Fatalf("validate FastOr result: %v", err)
	}
	if _, ok := got.highlowcontainer.containers[0].(*arrayContainer); !ok {
		t.Fatalf("ParOr sparse imported result has %T container, want *arrayContainer", got.highlowcontainer.containers[0])
	}
	if _, err := got.ToBytes(); err != nil {
		t.Fatalf("serialize ParOr result: %v", err)
	}
	if _, err := want.ToBytes(); err != nil {
		t.Fatalf("serialize FastOr result: %v", err)
	}
}

func TestParOrMultiwayBitmapOrFullUnion(t *testing.T) {
	if multiwayBitmapOrTestInputCount != multiwayBitmapOrInputCount {
		t.Fatalf("test uses %d inputs, fast path starts at %d", multiwayBitmapOrTestInputCount, multiwayBitmapOrInputCount)
	}

	const key = uint16(9)
	sources := make([]*Bitmap, 4)
	for phase := uint32(0); phase < uint32(len(sources)); phase++ {
		sources[phase] = NewBitmap()
		for low := phase; low <= MaxUint16; low += uint32(len(sources)) {
			sources[phase].Add(uint32(key)<<16 | low)
		}
		if _, ok := sources[phase].highlowcontainer.containers[0].(*bitmapContainer); !ok {
			t.Fatal("test input is not bitmap-backed")
		}
	}

	inputs := make([]*Bitmap, multiwayBitmapOrTestInputCount)
	inputSnapshots := make([]*Bitmap, len(inputs))
	for i := range inputs {
		inputs[i] = sources[i%len(sources)].Clone()
		inputSnapshots[i] = inputs[i].Clone()
	}

	got := ParOr(4, inputs...)
	want := NewBitmap()
	want.AddRange(uint64(key)<<16, uint64(key+1)<<16)
	if !got.Equals(want) {
		t.Fatal("ParOr full result differs from expected bitmap")
	}
	if got.GetCardinality() != want.GetCardinality() {
		t.Fatalf("ParOr cardinality %d, want %d", got.GetCardinality(), want.GetCardinality())
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("validate ParOr result: %v", err)
	}

	for i, input := range inputs {
		if !input.Equals(inputSnapshots[i]) {
			t.Fatalf("ParOr modified input %d", i)
		}
		if err := input.Validate(); err != nil {
			t.Fatalf("validate input %d after ParOr: %v", i, err)
		}
	}
}

func TestParOrMultiwayBitmapOrWorkerCountContract(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(multiwayBitmapOrMaxWorkers)
	previousDefault := defaultWorkerCount
	defaultWorkerCount = multiwayBitmapOrMaxWorkers
	t.Cleanup(func() {
		defaultWorkerCount = previousDefault
		runtime.GOMAXPROCS(previousProcs)
	})

	inputs, expected := newParOrHighFanInBitmapInputs()
	for _, test := range []struct {
		parallelism int
		want        int
	}{
		{parallelism: 2, want: 2},
		{parallelism: 3, want: 3},
		{parallelism: 4, want: 4},
		{parallelism: 0, want: 4},
		{parallelism: bitmapContainerSize, want: 4},
	} {
		got := ParOr(test.parallelism, inputs...)
		assertParOrResult(t, got, expected)

		parallelism := test.parallelism
		if parallelism == 0 {
			parallelism = defaultWorkerCount
		}
		if workers := multiwayBitmapOrWorkerCount(parallelism); workers != test.want {
			t.Errorf("ParOr(%d) selected %d workers, want %d", test.parallelism, workers, test.want)
		}
	}
}

func TestParOrMultiwayBitmapOrNegativeAndDefaultParallelism(t *testing.T) {
	if multiwayBitmapOrTestInputCount != multiwayBitmapOrInputCount {
		t.Fatalf("test uses %d inputs, fast path uses %d", multiwayBitmapOrTestInputCount, multiwayBitmapOrInputCount)
	}

	const key = uint16(13)
	serialized := newSerializedOneKeyBitmaps(t, key)
	want := FastOr(loadSerializedOneKeyBitmaps(t, serialized, multiwayBitmapOrTestInputCount)...)
	for _, parallelism := range []int{-512, -1, 0, 1, 2, 4} {
		inputs := loadSerializedOneKeyBitmaps(t, serialized, multiwayBitmapOrTestInputCount)
		got := ParOr(parallelism, inputs...)
		if !got.Equals(want) {
			t.Errorf("ParOr(%d) result differs from FastOr", parallelism)
		}
		if err := got.Validate(); err != nil {
			t.Errorf("validate ParOr(%d) result: %v", parallelism, err)
		}
	}
}

func TestParOrMultiwayBitmapOrSparseUnion(t *testing.T) {
	if multiwayBitmapOrTestInputCount != multiwayBitmapOrInputCount {
		t.Fatalf("test uses %d inputs, fast path uses %d", multiwayBitmapOrTestInputCount, multiwayBitmapOrInputCount)
	}

	const key = uint16(11)
	sourceContainer := newBitmapContainer()
	expected := NewBitmap()
	for low := uint32(0); low < 1<<16; low += 16 {
		sourceContainer.bitmap[low>>6] |= uint64(1) << (low & 63)
		expected.Add(uint32(key)<<16 | low)
	}
	sourceContainer.cardinality = arrayDefaultMaxSize
	source := &Bitmap{roaringArray{
		keys:            []uint16{key},
		containers:      []container{sourceContainer},
		needCopyOnWrite: make([]bool, 1),
	}}
	if err := source.Validate(); err != nil {
		t.Fatalf("validate bitmap-backed sparse input: %v", err)
	}

	inputs := make([]*Bitmap, multiwayBitmapOrTestInputCount)
	inputSnapshots := make([]*Bitmap, len(inputs))
	for i := range inputs {
		inputs[i] = source.Clone()
		inputSnapshots[i] = inputs[i].Clone()
	}

	got := ParOr(4, inputs...)
	if !got.Equals(expected) {
		t.Fatal("ParOr sparse result differs from expected bitmap")
	}
	if got.GetCardinality() != expected.GetCardinality() {
		t.Fatalf("ParOr cardinality %d, want %d", got.GetCardinality(), expected.GetCardinality())
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("validate ParOr sparse result: %v", err)
	}
	for i, input := range inputs {
		if !input.Equals(inputSnapshots[i]) {
			t.Fatalf("ParOr modified sparse input %d", i)
		}
	}
}
