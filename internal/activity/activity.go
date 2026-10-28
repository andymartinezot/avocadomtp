// Package activity tracks what the daemon is doing (phone, mounts, transfers)
// and publishes snapshots to watchers such as the menu bar app. It costs
// nothing while idle: snapshots go out on changes, plus once a second while a
// transfer is running.
package activity

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/andymartinezot/avocadomtp/internal/remote"
)

// Directions of a transfer.
const (
	ToPhone   = "toPhone"
	FromPhone = "fromPhone"
)

// Mount is a mounted storage.
type Mount struct {
	StorageID uint32 `json:"storageId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Backend   string `json:"backend"` // "fskit" or "nfs"
}

// TransferInfo is a transfer in a snapshot.
type TransferInfo struct {
	ID        uint64 `json:"id"`
	Path      string `json:"path"` // e.g. "Retroid6/DCIM/IMG_1.jpg"
	Direction string `json:"direction"`
	Bytes     int64  `json:"bytes"`
	Total     int64  `json:"total"` // 0 if unknown
	Rate      int64  `json:"rate"`  // bytes per second over the last tick
	State     string `json:"state"` // "active", "done" or "failed"
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"startedAt"` // Unix milliseconds
}

// State is a complete snapshot of the daemon's activity.
type State struct {
	Seq       uint64             `json:"seq"`
	Connected bool               `json:"connected"`
	Device    *remote.DeviceInfo `json:"device,omitempty"`
	Error     string             `json:"error,omitempty"`
	Asleep    bool               `json:"asleep,omitempty"` // plugged in but not answering
	Mounts    []Mount            `json:"mounts"`
	Transfers []TransferInfo     `json:"transfers"`
	RateIn    int64              `json:"rateIn"`  // bytes/s from the phone
	RateOut   int64              `json:"rateOut"` // bytes/s to the phone
}

// keepFinished is how long finished transfers stay in snapshots.
const keepFinished = 4 * time.Second

// Hub collects activity and fans snapshots out to subscribers. A nil *Hub is
// valid and does nothing.
type Hub struct {
	mu        sync.Mutex
	seq       uint64
	connected bool
	device    *remote.DeviceInfo
	errMsg    string
	asleep    bool
	mounts    []Mount
	transfers []*Transfer
	nextID    uint64
	subs      map[chan State]struct{}
	ticking   bool
	rateIn    int64
	rateOut   int64
}

func NewHub() *Hub { return &Hub{subs: map[chan State]struct{}{}} }

// Transfer is one file moving over the cable.
type Transfer struct {
	hub       *Hub
	id        uint64
	path      string
	direction string
	total     int64
	started   time.Time
	bytes     atomic.Int64

	// guarded by hub.mu
	lastBytes int64
	rate      int64
	state     string
	errMsg    string
	ended     time.Time
}

// Add records n transferred bytes. Safe on a nil Transfer and lock-free.
func (t *Transfer) Add(n int) {
	if t != nil {
		t.bytes.Add(int64(n))
	}
}

// End marks the transfer finished (failed if err is non-nil).
func (t *Transfer) End(err error) {
	if t == nil {
		return
	}
	h := t.hub
	h.mu.Lock()
	if t.state == "active" {
		t.state, t.ended = "done", time.Now()
		if err != nil {
			t.state, t.errMsg = "failed", err.Error()
		}
	}
	h.mu.Unlock()
	h.publish()
}

// Begin registers a transfer. total may be 0 if unknown.
func (h *Hub) Begin(path, direction string, total int64) *Transfer {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	h.nextID++
	t := &Transfer{hub: h, id: h.nextID, path: path, direction: direction, total: total,
		started: time.Now(), state: "active"}
	h.transfers = append(h.transfers, t)
	if !h.ticking {
		h.ticking = true
		go h.tick()
	}
	h.mu.Unlock()
	h.publish()
	return t
}

// SetPhone records the connection state. asleep means a phone is plugged in
// but doesn't answer.
func (h *Hub) SetPhone(connected bool, dev *remote.DeviceInfo, errMsg string, asleep bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.connected, h.device, h.errMsg, h.asleep = connected, dev, errMsg, asleep
	h.mu.Unlock()
	h.publish()
}

// SetMounts records the mounted volumes.
func (h *Hub) SetMounts(m []Mount) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.mounts = append([]Mount(nil), m...)
	h.mu.Unlock()
	h.publish()
}

// Snapshot returns the current state.
func (h *Hub) Snapshot() State {
	if h == nil {
		return State{Mounts: []Mount{}, Transfers: []TransferInfo{}}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotLocked()
}

func (h *Hub) snapshotLocked() State {
	st := State{Seq: h.seq, Connected: h.connected, Device: h.device, Error: h.errMsg, Asleep: h.asleep,
		Mounts: append([]Mount{}, h.mounts...), Transfers: []TransferInfo{}, RateIn: h.rateIn, RateOut: h.rateOut}
	for _, t := range h.transfers {
		st.Transfers = append(st.Transfers, TransferInfo{
			ID: t.id, Path: t.path, Direction: t.direction, Bytes: t.bytes.Load(), Total: t.total,
			Rate: t.rate, State: t.state, Error: t.errMsg, StartedAt: t.started.UnixMilli(),
		})
	}
	return st
}

// Subscribe returns a channel of snapshots, starting with the current one.
// Slow subscribers only ever see the latest snapshot. Call cancel when done.
func (h *Hub) Subscribe() (<-chan State, func()) {
	c := make(chan State, 1)
	if h == nil {
		c <- (*Hub)(nil).Snapshot()
		return c, func() {}
	}
	h.mu.Lock()
	h.subs[c] = struct{}{}
	c <- h.snapshotLocked()
	h.mu.Unlock()
	return c, func() {
		h.mu.Lock()
		delete(h.subs, c)
		h.mu.Unlock()
	}
}

func (h *Hub) publish() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publishLocked()
}

func (h *Hub) publishLocked() {
	h.seq++
	st := h.snapshotLocked()
	for c := range h.subs {
		select { // keep only the newest snapshot for slow readers
		case <-c:
		default:
		}
		c <- st
	}
}

// tick updates rates once a second while transfers exist, and drops finished
// transfers after keepFinished. It stops when there's nothing left to show.
func (h *Hub) tick() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := time.Now()
	for now := range t.C {
		secs := now.Sub(last).Seconds()
		last = now
		h.mu.Lock()
		var in, out int64
		kept := h.transfers[:0]
		for _, tr := range h.transfers {
			b := tr.bytes.Load()
			tr.rate = int64(float64(b-tr.lastBytes) / secs)
			tr.lastBytes = b
			if tr.state == "active" {
				if tr.direction == ToPhone {
					out += tr.rate
				} else {
					in += tr.rate
				}
			}
			if tr.state == "active" || now.Sub(tr.ended) < keepFinished {
				kept = append(kept, tr)
			}
		}
		for i := len(kept); i < len(h.transfers); i++ {
			h.transfers[i] = nil
		}
		h.transfers = kept
		h.rateIn, h.rateOut = in, out
		idle := len(h.transfers) == 0
		if idle {
			h.ticking = false
		}
		h.publishLocked()
		h.mu.Unlock()
		if idle {
			return
		}
	}
}
