package provider

// PLUGIN-SERVED (see AGENTS.md): Grok ("grok") is a retired built-in
// subscription, served by its plugin alone,
// the retired opencode-grok-auth (provider.BuiltinRetired). Its
// sign-in, models, requests and usage are the plugin's; what is left here
// reads the accounts signed in to the built-in for the move onto the plugin
// (migrate_side.go), installs and finds the Grok CLI the plugin signs in
// with, and shapes the plugin's request bodies (grokBody). A fix belongs in
// the plugin (github.com/dieqiyun/uni-switch, packages/grok), with the
// mover's min raised in internal/provider/migrate_side.go.

// GrokBase is the CLI's backend; a var so tests can point it elsewhere.
var GrokBase = "https://cli-chat-proxy.grok.com/v1"
