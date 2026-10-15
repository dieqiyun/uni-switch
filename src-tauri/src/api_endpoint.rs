//! Join a validated provider base URL without changing an explicit OpenAI API prefix.

pub(crate) fn openai(base: &str, chat: bool) -> String {
    let mut base = base.trim_end_matches('/');
    let mut explicit_resource = false;
    for suffix in ["/chat/completions", "/responses", "/messages"] {
        if let Some(root) = base.strip_suffix(suffix) {
            base = root;
            explicit_resource = true;
            break;
        }
    }
    let resource = if chat {
        "chat/completions"
    } else {
        "responses"
    };
    // An origin alone uses the conventional /v1 default. SDK-style base URLs
    // with a path are authoritative, including /api/paas/v4 and /backend-api/codex.
    let bare_origin = !explicit_resource
        && url::Url::parse(base).is_ok_and(|url| url.path().is_empty() || url.path() == "/");
    format!("{base}{}/{resource}", if bare_origin { "/v1" } else { "" })
}

pub(crate) fn messages(base: &str) -> String {
    let base = base.trim_end_matches('/');
    let base = base
        .strip_suffix("/chat/completions")
        .or_else(|| base.strip_suffix("/responses"))
        .unwrap_or(base);
    if base.ends_with("/messages") {
        base.to_owned()
    } else if base.ends_with("/v1") {
        format!("{base}/messages")
    } else {
        format!("{base}/v1/messages")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn usage_audit_endpoint_resources_keep_explicit_paths_and_native_messages_rules() {
        for (base, expected) in [
            ("https://example.test", "https://example.test/v1"),
            ("https://example.test/", "https://example.test/v1"),
            ("https://example.test/v1/", "https://example.test/v1"),
            (
                "https://example.test/api/paas/v4",
                "https://example.test/api/paas/v4",
            ),
            ("https://example.test/responses", "https://example.test"),
            (
                "https://example.test/chat/completions/",
                "https://example.test",
            ),
            (
                "https://example.test/gateway/messages",
                "https://example.test/gateway",
            ),
        ] {
            assert_eq!(openai(base, false), format!("{expected}/responses"));
            assert_eq!(openai(base, true), format!("{expected}/chat/completions"));
        }
        for (base, expected) in [
            ("https://example.test", "https://example.test/v1/messages"),
            (
                "https://example.test/gateway/v1",
                "https://example.test/gateway/v1/messages",
            ),
            (
                "https://example.test/gateway/messages/",
                "https://example.test/gateway/messages",
            ),
            (
                "https://example.test/gateway/responses",
                "https://example.test/gateway/v1/messages",
            ),
            (
                "https://example.test/gateway/v1/chat/completions",
                "https://example.test/gateway/v1/messages",
            ),
        ] {
            assert_eq!(messages(base), expected);
        }
    }
}
