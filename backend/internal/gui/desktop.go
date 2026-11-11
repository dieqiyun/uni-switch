package gui

// programName is the name uni-switch gives GTK on Linux (g_set_prgname, through
// Wails' LinuxOptions.ProgramName). GTK3 makes it the window's Wayland
// app_id and its X11 WM_CLASS ("uni-switch", "uni-switch"); without it both are the
// executable's name, uni-switch-linux-amd64 when the release is run as
// downloaded, which no desktop entry names, so the dock drew a gear (#2626).
// The entry registerScheme writes names it in StartupWMClass. WebKitGTK keys
// its data folder by it too (~/.local/share/uni-switch), see keepWebData.
const programName = "uni-switch"
