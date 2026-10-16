package usbx

/*
#include <libusb.h>
int avocado_register_hotplug(libusb_context *ctx, libusb_hotplug_callback_handle *handle);
*/
import "C"

import (
	"context"
	"errors"
	"sync"
)

var (
	hotplugMu   sync.Mutex
	hotplugSubs []chan struct{}
)

//export goHotplugCallback
func goHotplugCallback(ctx *C.libusb_context, dev *C.libusb_device, event C.libusb_hotplug_event, user *C.void) C.int {
	hotplugMu.Lock()
	for _, c := range hotplugSubs {
		select {
		case c <- struct{}{}:
		default: // a notification is already pending
		}
	}
	hotplugMu.Unlock()
	return 0 // keep the callback registered
}

// ErrNoHotplug is returned when libusb cannot deliver hotplug events.
var ErrNoHotplug = errors.New("libusb: hotplug notifications not supported")

// WatchHotplug returns a channel that receives a value whenever a USB device
// is attached or detached. Bursts are coalesced. The watcher thread blocks in
// libusb between events, so it costs nothing while the bus is quiet.
func WatchHotplug(ctx context.Context) (<-chan struct{}, error) {
	if C.libusb_has_capability(C.LIBUSB_CAP_HAS_HOTPLUG) == 0 {
		return nil, ErrNoHotplug
	}
	var uctx *C.libusb_context
	if err := check(C.libusb_init(&uctx)); err != nil {
		return nil, err
	}
	var handle C.libusb_hotplug_callback_handle
	if err := check(C.avocado_register_hotplug(uctx, &handle)); err != nil {
		C.libusb_exit(uctx)
		return nil, err
	}
	ch := make(chan struct{}, 1)
	hotplugMu.Lock()
	hotplugSubs = append(hotplugSubs, ch)
	hotplugMu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			C.libusb_handle_events(uctx) // blocks until an event or an interrupt
		}
	}()
	go func() {
		<-ctx.Done()
		C.libusb_hotplug_deregister_callback(uctx, handle)
		C.libusb_interrupt_event_handler(uctx)
		<-done
		hotplugMu.Lock()
		for i, c := range hotplugSubs {
			if c == ch {
				hotplugSubs = append(hotplugSubs[:i], hotplugSubs[i+1:]...)
				break
			}
		}
		hotplugMu.Unlock()
		C.libusb_exit(uctx)
	}()
	return ch, nil
}
