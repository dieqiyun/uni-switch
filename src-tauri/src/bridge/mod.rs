//! Go owns the local HTTP listener and every gateway rule. Rust only keeps the
//! desktop-owned core alive and transports startup/health messages over pipes.
pub mod reverse;
use crate::error::{AppError, Result};
use crate::{go_backend::GoBackend, store::Store};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::sync::{Arc, Mutex};
#[derive(Clone, Serialize, Deserialize)]
pub struct Route {
    pub port: u16,
    pub token: String,
}
impl Route {
    pub fn start(directory: &std::path::Path) -> Result<(Self, Option<()>)> {
        let v = crate::metadata_transport::call("gateway.listen", json!({"directory":directory}))?;
        let route = serde_json::from_value(v)
            .map_err(|_| AppError::new("go_gateway", "Go 网关启动响应无效"))?;
        Ok((route, Some(())))
    }
    #[cfg(test)]
    pub fn bind(directory: &std::path::Path) -> Result<(Self, ())> {
        Self::start(directory).map(|(r, _)| (r, ()))
    }
}
pub async fn healthy(route: &Route) -> bool {
    let route = route.clone();
    tokio::task::spawn_blocking(move || {
        crate::metadata_transport::call("gateway.healthy", json!({"route":route}))
    })
    .await
    .ok()
    .and_then(|r| r.ok())
    .and_then(|v| v.as_bool())
    .unwrap_or(false)
}
pub async fn maintain(
    _listener: Option<()>,
    _route: Route,
    store: Arc<Mutex<Store>>,
    _core: Arc<GoBackend>,
) {
    let directory = match store.lock() {
        Ok(s) => s.data_directory.clone(),
        Err(_) => return,
    };
    loop {
        let directory = directory.clone();
        let _ = tokio::task::spawn_blocking(move || {
            crate::metadata_transport::call("gateway.listen", json!({"directory":directory}))
        })
        .await;
        tokio::time::sleep(std::time::Duration::from_secs(2)).await;
    }
}
pub(crate) fn core_models(models: &[crate::types::ProviderModel]) -> Vec<Value> {
    models
        .iter()
        .map(|m| json!({"id":m.id,"enabled":m.enabled,"metadata":m.metadata}))
        .collect()
}
