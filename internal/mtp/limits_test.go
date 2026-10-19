package mtp

import (
	"errors"
	"testing"
)

func TestDatasetSizeLimit(t *testing.T) {
	var b []byte
	w := writerTo(&b)
	if _, err := w.Write(make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(make([]byte, maxDataset)); !errors.Is(err, ErrDatasetTooLarge) {
		t.Fatalf("expected ErrDatasetTooLarge, got %v", err)
	}
	if len(b) != 1024 {
		t.Fatalf("buffer grew past the limit: %d", len(b))
	}
}
