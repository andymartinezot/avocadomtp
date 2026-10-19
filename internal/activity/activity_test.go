package activity

import (
	"errors"
	"testing"
	"time"
)

func TestHubPublishesAndRates(t *testing.T) {
	h := NewHub()
	c, cancel := h.Subscribe()
	defer cancel()
	if st := <-c; st.Connected || len(st.Transfers) != 0 {
		t.Fatalf("initial %+v", st)
	}
	h.SetPhone(true, nil, "")
	if st := <-c; !st.Connected {
		t.Fatal("phone event missing")
	}
	h.SetMounts([]Mount{{StorageID: 1, Name: "SD", Path: "/x/SD", Backend: "fskit"}})
	if st := <-c; len(st.Mounts) != 1 {
		t.Fatal("mounts event missing")
	}

	tr := h.Begin("SD/a.bin", ToPhone, 1000)
	<-c
	tr.Add(400)
	// within ~1s a tick reports bytes and a rate
	deadline := time.After(3 * time.Second)
	for {
		select {
		case st := <-c:
			if len(st.Transfers) == 1 && st.Transfers[0].Bytes == 400 && st.Transfers[0].Rate > 0 && st.RateOut > 0 {
				goto ended
			}
		case <-deadline:
			t.Fatal("no rate update")
		}
	}
ended:
	tr.End(errors.New("cable pulled"))
	st := <-c
	if st.Transfers[0].State != "failed" || st.Transfers[0].Error == "" {
		t.Fatalf("end: %+v", st.Transfers[0])
	}
	// finished transfers disappear after keepFinished and the ticker stops
	deadline = time.After(keepFinished + 3*time.Second)
	for {
		select {
		case st := <-c:
			if len(st.Transfers) == 0 {
				h.mu.Lock()
				ticking := h.ticking
				h.mu.Unlock()
				if ticking {
					continue
				}
				return
			}
		case <-deadline:
			t.Fatal("finished transfer never dropped")
		}
	}
}

func TestNilHubIsSafe(t *testing.T) {
	var h *Hub
	tr := h.Begin("x", FromPhone, 1)
	tr.Add(1)
	tr.End(nil)
	h.SetPhone(true, nil, "")
	if st := h.Snapshot(); st.Connected {
		t.Fatal("nil hub reported state")
	}
}
