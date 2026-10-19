#include <libusb.h>

extern int goHotplugCallback(libusb_context *ctx, libusb_device *dev, libusb_hotplug_event event, void *user);

static int LIBUSB_CALL hotplug_trampoline(libusb_context *ctx, libusb_device *dev, libusb_hotplug_event event, void *user) {
	return goHotplugCallback(ctx, dev, event, user);
}

int avocadomtp_register_hotplug(libusb_context *ctx, libusb_hotplug_callback_handle *handle) {
	return libusb_hotplug_register_callback(ctx,
		LIBUSB_HOTPLUG_EVENT_DEVICE_ARRIVED | LIBUSB_HOTPLUG_EVENT_DEVICE_LEFT, 0,
		LIBUSB_HOTPLUG_MATCH_ANY, LIBUSB_HOTPLUG_MATCH_ANY, LIBUSB_HOTPLUG_MATCH_ANY,
		hotplug_trampoline, NULL, handle);
}
