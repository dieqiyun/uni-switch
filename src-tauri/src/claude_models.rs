//! Claude client model names and local context annotations. Keep upstream IDs intact.

pub(crate) const ROLES: &[&str] = &["sonnet", "opus", "haiku", "fable"];

pub(crate) fn split_context_marker(model: &str) -> (&str, bool) {
    let model = model.trim();
    if model
        .get(model.len().saturating_sub(4)..)
        .is_some_and(|suffix| suffix.eq_ignore_ascii_case("[1m]"))
    {
        (model[..model.len() - 4].trim_end(), true)
    } else {
        (model, false)
    }
}

pub(crate) fn desktop_model_valid(model: &str) -> bool {
    let normalized = model.to_ascii_lowercase();
    if normalized.contains("[1m]") {
        return false;
    }
    let tail = normalized.strip_prefix("anthropic/").unwrap_or(&normalized);
    ROLES.iter().any(|role| {
        tail.strip_prefix(&format!("claude-{role}-"))
            .is_some_and(|rest| !rest.is_empty())
    })
}

pub(crate) fn role(model: &str) -> Option<&'static str> {
    let (model, _) = split_context_marker(model);
    let normalized = model.to_ascii_lowercase();
    let model = normalized.strip_prefix("anthropic/").unwrap_or(&normalized);
    if let Some(role) = ROLES.iter().find(|role| **role == model) {
        return Some(*role);
    }
    let tail = model.strip_prefix("claude-")?;
    if let Some(role) = ROLES.iter().find(|role| {
        tail.strip_prefix(&format!("{role}-"))
            .is_some_and(|rest| !rest.is_empty())
    }) {
        return Some(*role);
    }
    // Older official IDs put a numeric version before the role, e.g. 3-5-haiku.
    if !tail.as_bytes().first().is_some_and(u8::is_ascii_digit) {
        return None;
    }
    tail.split('-')
        .find_map(|part| ROLES.iter().find(|role| **role == part).copied())
}

pub(crate) fn role_model<'a>(ids: &[&'a str], requested_role: &str) -> Option<&'a str> {
    ids.iter()
        .copied()
        .find(|id| role(id) == Some(requested_role))
        .or_else(|| {
            (requested_role == "fable")
                .then(|| ids.iter().copied().find(|id| role(id) == Some("opus")))
                .flatten()
        })
}

pub(crate) fn enabled_ids(provider: &crate::types::Provider) -> Vec<&str> {
    let mut ids = vec![provider.model.as_str()];
    for model in provider.codex_options.models.iter().filter(|m| m.enabled) {
        if !ids.contains(&model.id.as_str()) {
            ids.push(model.id.as_str());
        }
    }
    ids
}
