# Third-party notices

uni-switch's original application code is licensed under **AGPL-3.0-only**. Third-party components retain their original licenses; no third-party component is relicensed by this notice.

## OpenAI Codex base instructions

uni-switch includes the unchanged generic Codex base instructions from OpenAI Codex, tag `rust-v0.160.0`, file `codex-rs/models-manager/prompt.md`.

Source: https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/models-manager/prompt.md

OpenAI Codex · Copyright 2025 OpenAI. Licensed under the Apache License, Version 2.0. The complete license is included in `third-party/codex/LICENSE` in the source repository and `licenses/codex-LICENSE` in the installed application. The original text is embedded in the executable, used in generated model catalogs, and is not downloaded at runtime. No changes have been made to it.

## Dependency licenses

JavaScript and Rust dependencies retain their own licenses and copyright notices. Their exact versions are recorded in `pnpm-lock.yaml` and `src-tauri/Cargo.lock`. The bundled license texts and dependency source references are in `third-party/dependency-licenses.txt` in the source repository and `licenses/dependency-licenses.txt` in binary distributions.

The inventory includes development and build dependencies as well as runtime dependencies so that notices remain available for source builds. It is generated from locally installed package metadata and license files using `scripts/collect-dependency-licenses.py`; none of these files contains user credentials.

## Brand images

OpenAI, Anthropic, Z.ai, DeepSeek and Tencent WorkBuddy client marks belong to their respective owners, are used to identify supported clients, and are not covered by uni-switch's AGPL license. Sources are documented in `src/assets/brands/README.md`.

The purple 蝶祈云 logo belongs to 蝶祈云 and identifies the service website. It is not covered by the application's AGPL license. The application's own black-and-white uni-switch switching mark is part of this project.

No endorsement by any of these client owners is implied.
