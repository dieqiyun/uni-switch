# Statically compiled core modules

The third-party extension runtime is removed from this product. There is no package installer, market, Bun host, dynamic JavaScript agent/gateway module, plugin page/frame/service bridge, plugin sign-in endpoint, or plugin CLI command.

The packages under plugins/ (except the removed middleware module) are statically compiled core modules. internal/pluginrt and the window kernel register the sessions, library, privacy and other native pages. They are not installable extensions. Refresh uses compiled defaults; legacy plugins.yml cannot switch them off or configure them. The file is not deleted or rewritten at startup.

internal/plugin/removed.go and internal/agentplug/removed.go retain legacy record shapes and inert compatibility boundaries. They do not read plugin packages, load authentication, install, compile or execute code. Enumerations are empty and attempted execution or mutation fails explicitly. The management UI and API expose no extension operations.

Verification: scripts/qa-source-extensions.mjs, scripts/tests/no-extensions.test.mjs, TestUniSwitchRemovedExtensionsIgnoreSavedPackages and TestUniSwitchExtensionEndpointsRemoved.

The independent Routing module and its client assets are removed. Settings > Gateway owns the collapsible Request records section; routing execution and its local diagnostic data remain core services.

The gateway module registers configuration only, with no nav.page part. Its client helpers mount the static Settings > Gateway panel. The shell refreshes its provider state on entry and window refresh; leaving the panel stops request/key polling.
