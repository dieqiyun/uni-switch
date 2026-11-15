# Subscription ownership after extension removal

Third-party provider extensions are not supported. Their installation, sign-in, refresh, migration, execution and management entrypoints have been removed. Requests cannot start the old host. The legacy data files are left on disk, and the compatibility enumeration supplies no providers. There is no automatic migration or installation.

Native provider accounts and API keys still use internal/provider. Subscriptions retired from the native registry and served exclusively by third-party extensions are unavailable. This does not remove the native client configuration adapters for similarly named apps. Do not promise a retired subscription merely because its client adapter exists.

See [the core module boundary](plugin-runtime.md) and [product removal scope](../../../docs/extensions-removal.md).
