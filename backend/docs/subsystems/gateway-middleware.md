# Core gateway middleware

The gateway retains its native middleware boundary for built-in request/response processing. Configurable privacy masking, its middleware interfaces and local identification models are removed. Diagnostic credential cleanup is separate and never changes forwarded content; see [the removal boundary](masking-removed.md). Third-party JavaScript middleware, custom routing strategies and extension usage observers have been removed, including internal/middleware, internal/jsmod and plugins/middleware.

No extension source file is compiled or executed. Requests use the native gateway and its static modules; extension hook enumeration is empty. Protocol conversion, streaming, tool calls and routing continue through their existing core implementation.

See [the core module boundary](plugin-runtime.md).
