// clipboard_probe: a GTK 3 window that copies a text once it has the
// focus, then reads the clipboard back through the compositor with
// uni_switch_clipboard_read and prints what it read (TestClipboardReadBack).
// "wails" copies as Wails v3 does (CLIPBOARD, then PRIMARY), "uni-switch" as
// uni-switch does (uni_switch_clipboard_set). "again" copies and reads back as
// uni-switch does, several times over while the window keeps the focus, and
// prints "alive" if GTK's connection to the compositor is still up after
// them (#2039).
#include "clipboard_gtk3.h"
#include <stdio.h>

static const char *mode;
static const char *text;
static void *display, *seat;
static int done;

static gboolean alive(gpointer data) {
	printf("alive\n");
	fflush(stdout);
	gtk_main_quit();
	return G_SOURCE_REMOVE;
}

// burst has the compositor send 4 offers that are alive at once, on data
// devices of the probe's own: it has to give them object ids past every id
// the probe's connection has seen. With an earlier read back's device left
// alive in the compositor (#2039), those ids are out of step with the
// client's and libwayland fails the connection with EINVAL. It returns
// how many offers came.
static int burst(void) {
	struct wl_display *d = display;
	struct wl_event_queue *q = wl_display_create_queue(d);
	struct wl_display *w = wl_proxy_create_wrapper(d);
	uni_switch_probe p[4];
	struct wl_data_device *dev[4];
	int got = 0;
	memset(p, 0, sizeof p);
	wl_proxy_set_queue((struct wl_proxy *)w, q);
	struct wl_registry *reg = wl_display_get_registry(w);
	wl_registry_add_listener(reg, &uni_switch_registry_listener, &p[0]);
	if (wl_display_roundtrip_queue(d, q) < 0 || !p[0].manager)
		return -1;
	for (int i = 0; i < 4; i++) {
		dev[i] = wl_data_device_manager_get_data_device(p[0].manager, seat);
		wl_data_device_add_listener(dev[i], &uni_switch_device_listener, &p[i]);
	}
	if (wl_display_roundtrip_queue(d, q) < 0)
		return -1;
	for (int i = 0; i < 4; i++) {
		got += p[i].n;
		for (int j = 0; j < p[i].n; j++)
			wl_data_offer_destroy(p[i].offers[j]);
		wl_data_device_release(dev[i]);
	}
	wl_data_device_manager_destroy(p[0].manager);
	wl_registry_destroy(reg);
	wl_proxy_wrapper_destroy(w);
	wl_display_flush(d);
	wl_event_queue_destroy(q);
	return got;
}

static gboolean copy_more(gpointer data) {
	uni_switch_clipboard_set(data);
	g_free(data);
	return G_SOURCE_REMOVE;
}

static gpointer reader(gpointer data) {
	char *out = NULL;
	size_t n = 0;
	int r = uni_switch_clipboard_read(display, seat, 3000, &out, &n);
	printf("read=%d len=%zu text=%s\n", r, n, out ? out : "");
	fflush(stdout);
	free(out);
	// PROBE_HOLD=<seconds> keeps the window, and its copy, for a paste
	// from outside (wl-paste) by hand
	const char *hold = getenv("PROBE_HOLD");
	if (hold)
		g_usleep(atoi(hold) * G_USEC_PER_SEC);
	if (strcmp(mode, "again") == 0) {
		// each copy sends every data device of the focused client a
		// new offer, any device left alive in the compositor among them
		for (int k = 1; k <= 8; k++) {
			g_idle_add(copy_more, g_strdup_printf("%s %d", text, k));
			g_usleep(150 * 1000);
			r = uni_switch_clipboard_read(display, seat, 3000, &out, &n);
			printf("again=%d len=%zu\n", r, n);
			fflush(stdout);
			free(out);
			out = NULL;
		}
		printf("burst=%d\n", burst());
		fflush(stdout);
		g_timeout_add(1500, alive, NULL);
		return NULL;
	}
	g_idle_add((GSourceFunc)gtk_main_quit, NULL);
	return NULL;
}

static gboolean copy(gpointer data) {
	if (done++)
		return G_SOURCE_REMOVE;
	if (strcmp(mode, "wails") == 0) {
		gtk_clipboard_set_text(gtk_clipboard_get(GDK_SELECTION_CLIPBOARD), text, -1);
		gtk_clipboard_set_text(gtk_clipboard_get(GDK_SELECTION_PRIMARY), text, -1);
	} else {
		uni_switch_clipboard_set(text);
	}
	if (!uni_switch_clipboard_wayland(&display, &seat)) {
		printf("read=-1 len=0 text= (not wayland)\n");
		gtk_main_quit();
		return G_SOURCE_REMOVE;
	}
	g_thread_unref(g_thread_new("reader", reader, NULL));
	return G_SOURCE_REMOVE;
}

// primary says whether the compositor has a primary selection protocol,
// from a connection of its own
static void primary_global(void *data, struct wl_registry *r, uint32_t name, const char *iface, uint32_t v) {
	if (strcmp(iface, "zwp_primary_selection_device_manager_v1") == 0 || strcmp(iface, "gtk_primary_selection_device_manager") == 0)
		*(int *)data = 1;
}
static void primary_remove(void *data, struct wl_registry *r, uint32_t name) {}
static const struct wl_registry_listener primary_listener = {primary_global, primary_remove};
static int primary(void) {
	int has = 0;
	struct wl_display *d = wl_display_connect(NULL);
	if (!d)
		return -1;
	struct wl_registry *r = wl_display_get_registry(d);
	wl_registry_add_listener(r, &primary_listener, &has);
	wl_display_roundtrip(d);
	wl_registry_destroy(r);
	wl_display_disconnect(d);
	return has;
}

static gboolean focused(GtkWidget *w, GdkEvent *e, gpointer data) {
	// the copy goes out from the main loop, as the app's does
	g_idle_add(copy, NULL);
	return FALSE;
}

int main(int argc, char **argv) {
	gtk_init(&argc, &argv);
	printf("primary=%d\n", primary());
	mode = argc > 1 ? argv[1] : "uni-switch";
	text = argc > 2 ? argv[2] : "uni-switch probe text";
	GtkWidget *w = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_default_size(GTK_WINDOW(w), 200, 100);
	g_signal_connect(w, "focus-in-event", G_CALLBACK(focused), NULL);
	gtk_widget_show_all(w);
	g_timeout_add_seconds(5, copy, NULL);
	g_timeout_add_seconds(getenv("PROBE_HOLD") ? 60 : 20, (GSourceFunc)gtk_main_quit, NULL);
	gtk_main();
	return 0;
}
