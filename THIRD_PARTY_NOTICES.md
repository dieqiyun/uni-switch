# Third-party notices

uni-switch's original application code is licensed under **AGPL-3.0-only**. Third-party components retain their original licenses; no third-party component is relicensed by this notice.

## OpenAI Codex base instructions

uni-switch includes the unchanged generic Codex base instructions from OpenAI Codex, tag `rust-v0.160.0`, file `codex-rs/models-manager/prompt.md`.

Source: https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/models-manager/prompt.md

OpenAI Codex · Copyright 2025 OpenAI. Licensed under the Apache License, Version 2.0. The complete license is included in `third-party/codex/LICENSE` in the source repository and `licenses/codex-LICENSE` in the installed application. The original text is embedded in the executable, used in generated model catalogs, and is not downloaded at runtime. No changes have been made to it.

## Dependency licenses

JavaScript, Rust and Go dependencies retain their own licenses and copyright notices. Their exact versions are recorded in `pnpm-lock.yaml`, `src-tauri/Cargo.lock`, `backend/go.mod` and `backend/go.sum`. The bundled license texts and dependency source references are in `third-party/dependency-licenses.txt` in the source repository and `licenses/dependency-licenses.txt` in binary distributions.

The inventory includes development and build dependencies as well as runtime dependencies so that notices remain available for source builds. It is generated from locally installed package metadata and license files using `scripts/collect-dependency-licenses.py`; none of these files contains user credentials.

## Go runtime and standard library

The independently implemented Go core embeds the Go runtime. TOML parsing uses `github.com/pelletier/go-toml/v2`, while our own span editor preserves original text. YAML editing uses `gopkg.in/yaml.v3`; existing SQLite data is handled by the pure-Go `modernc.org/sqlite` driver and its locked dependencies. Their complete upstream license texts are included in the dependency inventory. Magpie is not a Go module dependency. Native CI pins Go 1.26.8. Go source: https://go.dev/src/ (BSD-3-Clause). The following notice applies to the Go runtime and standard library, not to uni-switch application code.

```text
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## Magpie reference logic, public preset data and test vectors

The Go model-metadata, protocol gateway, client configuration and atomic-file implementations reference reviewed catalog/provider, gateway, agent and edit logic in `yetone/magpie`, commit `665b9aff126e26eb5fa8931b7395feb6002185f3`. Public endpoint/catalog preset data in `backend/internal/modelmeta/presets.json` was extracted from that commit's `internal/provider/presets.go`. Model-metadata reference test vectors were generated from its pure functions by an isolated QA program. Source file hashes and the scope of comparison are recorded in `docs/magpie-modelmeta-reference.json`, `docs/magpie-gateway-reference.json`, `docs/model-metadata.md` and `docs/magpie-capability-alignment.md`. These records do not assert complete source or behavior equivalence to Magpie.

Source: https://github.com/yetone/magpie/tree/665b9aff126e26eb5fa8931b7395feb6002185f3

The subscription account flows, Gemini native gateway and image/video/native transports additionally reference the reviewed provider sign-in, Code Assist, Claude CLI/MCP and media logic at that same commit. They are independently implemented in this project's Go packages. The 59 fixed-reference file hashes and the implemented scope are recorded in `docs/magpie-platform-reference.json` and `docs/unified-gateway.md`; this does not assert coverage of every upstream provider, subscription or media branch. Google OAuth registration is supplied by the user; third-party Google client identifiers and secrets are not distributed.

The expanded third-party client configuration adapters additionally reference the complete client registration table and reviewed agent, Muse/Cursor model catalogs and flat model-name logic at commit `f13d5f16f474f9c3baa5081c6cae3934af291cfd`. The fixed source hashes and per-client mapping are in `docs/magpie-clients-reference.json`; coverage and native verification limits are in `docs/magpie-client-support.md`. Source: https://github.com/yetone/magpie/tree/f13d5f16f474f9c3baa5081c6cae3934af291cfd

Magpie is not a runtime or normal build dependency. The following original MIT notice applies to the reference material and derived portions; uni-switch's original code remains AGPL-3.0-only.

```text
MIT License

Copyright (c) 2026 yetone

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Brand images

OpenAI, Anthropic, Z.ai, DeepSeek and Tencent WorkBuddy client marks belong to their respective owners, are used to identify supported clients, and are not covered by uni-switch's AGPL license. Sources are documented in `src/assets/brands/README.md`.

The purple 蝶祈云 logo belongs to 蝶祈云 and identifies the service website. It is not covered by the application's AGPL license. The application's own black-and-white uni-switch switching mark is part of this project.

No endorsement by any of these client owners is implied.
