// Copying to the clipboard under GTK 3, and reading the copy back through
// the compositor on Wayland (#1881).
//
// GTK 3 (every 3.24 release) keeps one list of the targets it serves for
// the CLIPBOARD and PRIMARY selections together (GdkWaylandSelection's
// source_targets). Setting PRIMARY empties that list, and refills it only
// when the compositor has a primary selection protocol. KDE Plasma with
// middle-click paste turned off has none (kwinrc [Wayland]
// EnablePrimarySelection=false), so text set on CLIPBOARD and then on
// PRIMARY, as Wails does, is offered with no targets left to serve: every
// paste asks for it and GTK closes the pipe empty. PRIMARY first, then
// CLIPBOARD, leaves CLIPBOARD's targets in the list on every compositor.
#include <gtk/gtk.h>
#ifdef GDK_WINDOWING_WAYLAND
#include <gdk/gdkwayland.h>
#include <wayland-client.h>
#endif
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

// uni_switch_clipboard_set puts text on PRIMARY and then CLIPBOARD. It runs on
// GTK's main thread.
static void uni_switch_clipboard_set(const char *text) {
	gtk_clipboard_set_text(gtk_clipboard_get(GDK_SELECTION_PRIMARY), text, -1);
	gtk_clipboard_set_text(gtk_clipboard_get(GDK_SELECTION_CLIPBOARD), text, -1);
}

// uni_switch_clipboard_wayland gives GDK's Wayland display and seat, for
// uni_switch_clipboard_read, or returns 0 when GDK isn't on Wayland. It runs on
// GTK's main thread.
static int uni_switch_clipboard_wayland(void **display, void **seat) {
#ifdef GDK_WINDOWING_WAYLAND
	GdkDisplay *d = gdk_display_get_default();
	if (d && GDK_IS_WAYLAND_DISPLAY(d)) {
		GdkSeat *s = gdk_display_get_default_seat(d);
		*display = gdk_wayland_display_get_wl_display(d);
		*seat = s ? gdk_wayland_seat_get_wl_seat(s) : NULL;
		return *display && *seat;
	}
#endif
	return 0;
}

#ifdef GDK_WINDOWING_WAYLAND
#define UNI_SWITCH_PROBE_OFFERS 16

typedef struct {
	struct wl_data_device_manager *manager;
	uint32_t version;
	struct wl_data_offer *offers[UNI_SWITCH_PROBE_OFFERS];
	int text[UNI_SWITCH_PROBE_OFFERS]; // 1 utf-8, 2 UTF8_STRING, 3 text/plain
	int n;
	struct wl_data_offer *selection;
	int got_selection;
} uni_switch_probe;

static int uni_switch_probe_index(uni_switch_probe *p, struct wl_data_offer *o) {
	for (int i = 0; i < p->n; i++)
		if (p->offers[i] == o)
			return i;
	return -1;
}

static void uni_switch_offer_offer(void *data, struct wl_data_offer *o, const char *mime) {
	uni_switch_probe *p = data;
	int i = uni_switch_probe_index(p, o), k = 0;
	if (i < 0)
		return;
	if (strcmp(mime, "text/plain;charset=utf-8") == 0)
		k = 1;
	else if (strcmp(mime, "UTF8_STRING") == 0)
		k = 2;
	else if (strcmp(mime, "text/plain") == 0)
		k = 3;
	if (k && (!p->text[i] || k < p->text[i]))
		p->text[i] = k;
}
static void uni_switch_offer_actions(void *data, struct wl_data_offer *o, uint32_t a) {}
static const struct wl_data_offer_listener uni_switch_offer_listener = {
	uni_switch_offer_offer, uni_switch_offer_actions, uni_switch_offer_actions,
};

static void uni_switch_device_offer(void *data, struct wl_data_device *d, struct wl_data_offer *o) {
	uni_switch_probe *p = data;
	if (p->n == UNI_SWITCH_PROBE_OFFERS) {
		wl_data_offer_destroy(o);
		return;
	}
	p->offers[p->n] = o;
	p->text[p->n] = 0;
	p->n++;
	wl_data_offer_add_listener(o, &uni_switch_offer_listener, p);
}
static void uni_switch_device_enter(void *data, struct wl_data_device *d, uint32_t serial, struct wl_surface *s, wl_fixed_t x, wl_fixed_t y, struct wl_data_offer *o) {}
static void uni_switch_device_leave(void *data, struct wl_data_device *d) {}
static void uni_switch_device_motion(void *data, struct wl_data_device *d, uint32_t t, wl_fixed_t x, wl_fixed_t y) {}
static void uni_switch_device_drop(void *data, struct wl_data_device *d) {}
static void uni_switch_device_selection(void *data, struct wl_data_device *d, struct wl_data_offer *o) {
	uni_switch_probe *p = data;
	p->selection = o;
	p->got_selection = 1;
}
static const struct wl_data_device_listener uni_switch_device_listener = {
	uni_switch_device_offer, uni_switch_device_enter, uni_switch_device_leave,
	uni_switch_device_motion, uni_switch_device_drop, uni_switch_device_selection,
};

static void uni_switch_registry_global(void *data, struct wl_registry *r, uint32_t name, const char *iface, uint32_t version) {
	uni_switch_probe *p = data;
	if (!p->manager && strcmp(iface, "wl_data_device_manager") == 0) {
		p->version = version < 3 ? version : 3;
		p->manager = wl_registry_bind(r, name, &wl_data_device_manager_interface, p->version);
	}
}
static void uni_switch_registry_remove(void *data, struct wl_registry *r, uint32_t name) {}
static const struct wl_registry_listener uni_switch_registry_listener = {
	uni_switch_registry_global, uni_switch_registry_remove,
};

static long uni_switch_ms_since(struct timespec *t0) {
	struct timespec t;
	clock_gettime(CLOCK_MONOTONIC, &t);
	return (t.tv_sec - t0->tv_sec) * 1000 + (t.tv_nsec - t0->tv_nsec) / 1000000;
}
#endif

// uni_switch_clipboard_read reads the clipboard the way another program
// pasting would: from the compositor, through a data device of its own on
// its own event queue. It runs off GTK's main thread, which has to be free
// to answer when uni-switch holds the clipboard itself. It returns 1 with the
// text in *out (free it), 0 when the clipboard holds something else or
// nothing could be read from it, and -1 when it can't tell: no Wayland,
// or the compositor sent no selection (it sends one only to the focused
// client), and -2 when the compositor's data devices can't be released
// (wl_data_device_manager version 1), so nothing was read.
//
// The data device is released, not just forgotten (#2039). A device
// dropped with wl_data_device_destroy stays alive in the compositor, which
// goes on sending it a new wl_data_offer for every later clipboard change.
// libwayland drops those events unread without taking the offer's object
// id, the next object the compositor creates is then out of sequence, and
// the display connection fails with EINVAL: GTK prints "Error reading
// events from display: Invalid argument" and the app exits.
static int uni_switch_clipboard_read(void *display, void *seat, int timeout_ms, char **out, size_t *len) {
	*out = NULL;
	*len = 0;
#ifdef GDK_WINDOWING_WAYLAND
	struct wl_display *d = display;
	struct wl_event_queue *q = wl_display_create_queue(d);
	struct wl_display *w = wl_proxy_create_wrapper(d);
	uni_switch_probe p;
	memset(&p, 0, sizeof p);
	struct wl_registry *reg;
	struct wl_data_device *dev = NULL;
	int ret = -1, i, eof = 0, fds[2];
	const char *mime;
	size_t cap = 4096, n = 0;
	char *buf;
	struct timespec t0;

	wl_proxy_set_queue((struct wl_proxy *)w, q);
	reg = wl_display_get_registry(w);
	wl_registry_add_listener(reg, &uni_switch_registry_listener, &p);
	if (wl_display_roundtrip_queue(d, q) < 0 || !p.manager)
		goto done;
	if (p.version < WL_DATA_DEVICE_RELEASE_SINCE_VERSION) {
		ret = -2;
		goto done;
	}
	dev = wl_data_device_manager_get_data_device(p.manager, seat);
	wl_data_device_add_listener(dev, &uni_switch_device_listener, &p);
	if (wl_display_roundtrip_queue(d, q) < 0)
		goto done;
	if (!p.got_selection)
		goto done;
	ret = 0;
	i = p.selection ? uni_switch_probe_index(&p, p.selection) : -1;
	if (i < 0 || !p.text[i])
		goto done;
	mime = p.text[i] == 1 ? "text/plain;charset=utf-8" : p.text[i] == 2 ? "UTF8_STRING" : "text/plain";
	if (pipe(fds) < 0)
		goto done;
	fcntl(fds[0], F_SETFD, FD_CLOEXEC);
	fcntl(fds[1], F_SETFD, FD_CLOEXEC);
	wl_data_offer_receive(p.selection, mime, fds[1]);
	close(fds[1]);
	wl_display_flush(d);

	buf = malloc(cap);
	clock_gettime(CLOCK_MONOTONIC, &t0);
	while (buf) {
		long left = timeout_ms - uni_switch_ms_since(&t0);
		struct pollfd pfd = {fds[0], POLLIN, 0};
		if (left <= 0)
			break;
		// keep the request going out: the compositor may not have
		// read it yet when the first flush met a full socket
		wl_display_flush(d);
		int r = poll(&pfd, 1, left < 50 ? (int)left : 50);
		if (r < 0 && errno != EINTR)
			break;
		if (r <= 0)
			continue;
		if (n + 1024 > cap) {
			char *more = cap < (64u << 20) ? realloc(buf, cap * 2) : NULL;
			if (!more)
				break;
			buf = more;
			cap *= 2;
		}
		ssize_t k = read(fds[0], buf + n, cap - n - 1);
		if (k < 0 && errno == EINTR)
			continue;
		if (k <= 0) {
			eof = k == 0;
			break;
		}
		n += k;
	}
	close(fds[0]);
	if (buf && eof) {
		buf[n] = 0;
		*out = buf;
		*len = n;
		ret = 1;
	} else {
		free(buf);
		// no end in time: whoever holds it didn't answer, so it can't
		// be told what it holds
		if (!eof)
			ret = -1;
	}

done:
	for (int j = 0; j < p.n; j++)
		wl_data_offer_destroy(p.offers[j]);
	if (dev)
		wl_data_device_release(dev);
	if (p.manager)
		wl_data_device_manager_destroy(p.manager);
	wl_registry_destroy(reg);
	wl_proxy_wrapper_destroy(w);
	wl_display_flush(d);
	wl_event_queue_destroy(q);
	return ret;
#else
	return -1;
#endif
}
