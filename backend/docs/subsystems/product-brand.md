# Product identity

The shipped product is `uni-switch`. Its UI, CLI, browser manifest, native
resources, generated client configuration and local integration identifiers
use this name at their source, rather than rewriting rendered text.

The Go module is `github.com/dieqiyun/uni-switch/backend`. Internal Go names
use `UniSwitch` / `uniSwitch`; environment variables use `UNI_SWITCH_`;
request headers use `X-Uni-Switch-`; JSON uses `uniSwitchUserAgent`.
Import links use `uni-switch://`, gateway metadata uses `/v1/uni-switch/`,
without an outbound remote gateway preset. Its legacy preset identifiers
are recognized only to keep old records inactive.

Client adapters write the `uni-switch` provider namespace, model catalog
filenames and configuration markers. Disconnect and rollback still preserve
user edits. No running client or existing user configuration is modified by
this source change or by its isolated QA checks. Existing saved provider
endpoints are retained; removed vendor presets cannot be added implicitly.

The website and release links point to
`https://github.com/dieqiyun/uni-switch`. No upstream icon proxy or update
feed is contacted by default. `UNI_SWITCH_ICON_PROXY`, `UNI_SWITCH_UPDATE_FEED`
and `UNI_SWITCH_NOTES_FEED` are explicit overrides. Native desktop updates
use the release page. Unavailable upstream setup/quota skill recommendations
and plugin-list mirrors are removed instead of inventing replacement URLs.
Retired migration records remain readable, but do not name installable
replacement packages. Third-party extension execution stays disabled.

Original upstream notices, licenses and captured third-party test inputs
remain accurate attribution and provenance. `docs/upstream-source.json`
retains original hashes and records local adaptations and renamed files
separately (`replacementPath`).

Verification uses `scripts/tests/product-brand.test.mjs`, isolated Go
adapter tests, `scripts/qa-source-brand.mjs` and
`scripts/qa-source-directory.mjs`. The UI checks use Chromium and WebKit
in all five supported languages. Test credentials and configuration live
under `.qa`, never in the user's client folders.
