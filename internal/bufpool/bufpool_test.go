package bufpool

import "testing"

func TestGetPut(t *testing.T) {
	for _, n := range []int{1, 64 << 10, 64<<10 + 1, 3 << 20, 16 << 20} {
		b := Get(n)
		if len(b) != n || cap(b)&(cap(b)-1) != 0 || cap(b) < n {
			t.Fatalf("Get(%d): len %d cap %d", n, len(b), cap(b))
		}
		Put(b)
	}
	if b := Get(17 << 20); len(b) != 17<<20 {
		t.Fatal("oversize")
	}
	Put(make([]byte, 100)) // foreign buffers are ignored
}

func BenchmarkGetPut(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Put(Get(8 << 20))
	}
}
