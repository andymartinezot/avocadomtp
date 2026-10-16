package mtptest

import (
	"testing"

	"github.com/andymartinezot/avocadomtp/internal/mtp"
	"github.com/andymartinezot/avocadomtp/internal/remote"
)

// Phone is an MTP session over a Fake that satisfies remote.Remote.
type Phone struct {
	*mtp.Session
	Fake *Fake
}

// NewPhone opens a session with a new fake device.
func NewPhone(t testing.TB) *Phone {
	f := New(t)
	s, err := mtp.Open(f, f.MPS)
	if err != nil {
		t.Fatal(err)
	}
	return &Phone{Session: s, Fake: f}
}

func (p *Phone) Device() remote.DeviceInfo {
	return remote.DeviceInfo{Manufacturer: "Fake", Model: "Phone"}
}
func (p *Phone) Healthy() error { return nil }
func (p *Phone) Close()         {}
