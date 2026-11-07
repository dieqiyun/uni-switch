//! UI consent boundary for Go's Magpie-style tiny connection test.
use crate::error::{AppError, Result};
use serde::{Deserialize, Serialize};
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum Endpoint {
    Messages,
    ChatCompletions,
    Responses,
}
#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Observation {
    pub model: String,
    pub endpoint: Endpoint,
    pub state: String,
    pub checked_at: u64,
    pub message: String,
}
pub fn require_consent(consent: bool) -> Result<()> {
    if consent {
        Ok(())
    } else {
        Err(AppError::new(
            "verification_consent",
            "请先确认测试可能计费",
        ))
    }
}
