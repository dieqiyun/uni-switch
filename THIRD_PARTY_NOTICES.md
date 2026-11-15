# Third-party notices

uni-switch original code and integration adapters are licensed under AGPL-3.0-only. Third-party components retain their original licenses.

## Magpie source import

Directly incorporated source from https://github.com/yetone/magpie at commit `30b7a17326ac1cad40ca349059c3348a2580938b` (2026-11-08).

Copyright (c) 2026 yetone. Licensed under MIT. The complete original license is in `backend/LICENSE`. Original dependency notices are in `backend/THIRD_PARTY_NOTICES`, embedded in our core and available through the application's licenses API. The source, tests, native resources and built-in plugins are compiled locally, not acquired from an installed Magpie executable or runtime service.

The original Go module/import namespace is retained to minimize source changes. Original protocol names, configuration identifiers, plugin namespaces and public OAuth client registrations remain for compatibility. They identify third-party formats or public client registrations, not private user tokens.

The import and local adaptations are recorded with per-file hashes in `docs/upstream-source.json`. Material business implementations under gateway, provider, catalog, agent and edit remain byte-identical to the fixed source. Local changes cover application data isolation, native-shell lifecycle, product distribution boundaries and UI presentation.

## Dependencies and brand assets

Go dependencies are locked in `backend/go.mod` and `backend/go.sum`; bundled runtime notices are in `backend/THIRD_PARTY_NOTICES`. Native and frontend dependencies are locked in `src-tauri/Cargo.lock` and `pnpm-lock.yaml`. `third-party/dependency-licenses.txt` records the previous release's collected license report; regenerate it when preparing a new release.

Third-party client/provider marks remain the property of their owners. Displaying them identifies the corresponding client or service and does not imply endorsement.
