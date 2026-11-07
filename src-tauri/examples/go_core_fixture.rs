//! Isolated transport fixture: no Store, real credentials or client files.
use std::io::{BufRead, Write};
use uni_switch_lib::{error::Result, go_backend::GoBackend, types::BalanceQuery};

#[tokio::main]
async fn main() {
    let directory = std::env::args_os()
        .nth(2)
        .expect("isolated metadata data directory");
    uni_switch_lib::model_registry::initialize(std::path::Path::new(&directory));
    let core = std::sync::Arc::new(GoBackend::new(
        std::env::args_os()
            .nth(1)
            .expect("Go core executable path")
            .into(),
    ));
    for line in std::io::stdin().lock().lines() {
        let line = line.expect("fixture input");
        let request: serde_json::Value = serde_json::from_str(&line).expect("fixture JSON");
        let base = request["baseUrl"].as_str().expect("base URL");
        let key = request["apiKey"].as_str().expect("synthetic key");
        let result: Result<serde_json::Value> = match request["method"].as_str() {
            Some("gatewayCancel") => match core.clone().gateway(request["call"].clone()).await {
                Ok(response) => {
                    drop(response);
                    Ok(serde_json::json!({"cancelled":true}))
                }
                Err(e) => Err(e),
            },
            Some("gateway") => {
                let response = core.clone().gateway(request["call"].clone()).await;
                match response {
                    Ok(response) => {
                        let status = response.status().as_u16();
                        match axum::body::to_bytes(response.into_body(), 16 * 1024 * 1024).await {
                            Ok(body) => Ok(
                                serde_json::json!({"status":status,"body":String::from_utf8_lossy(&body)}),
                            ),
                            Err(_) => Err(uni_switch_lib::error::AppError::new(
                                "fixture",
                                "gateway body failed",
                            )),
                        }
                    }
                    Err(e) => Err(e),
                }
            }
            Some("resolve") => {
                let mut models: Vec<uni_switch_lib::types::ProviderModel> =
                    serde_json::from_value(request["models"].clone()).expect("fixture models");
                uni_switch_lib::model_registry::resolve_models(
                    &mut models,
                    base,
                    request["providerContext"].as_i64(),
                )
                .and_then(|()| {
                    serde_json::to_value(models).map_err(|_| {
                        uni_switch_lib::error::AppError::new("fixture", "result serialization")
                    })
                })
            }
            Some("discover") => core.discover(base, key, None, None).await.and_then(|r| {
                serde_json::to_value(r).map_err(|_| {
                    uni_switch_lib::error::AppError::new("fixture", "result serialization")
                })
            }),
            Some("balance") => {
                let query: Option<BalanceQuery> = request
                    .get("query")
                    .cloned()
                    .map(serde_json::from_value)
                    .transpose()
                    .expect("query");
                core.balance(base, key, query, request["accountToken"].as_str())
                    .await
                    .and_then(|r| {
                        serde_json::to_value(r).map_err(|_| {
                            uni_switch_lib::error::AppError::new("fixture", "result serialization")
                        })
                    })
            }
            Some("cancel") => {
                tokio::select! {
                    _ = core.discover(base, key, None, None) => panic!("slow fixture completed before cancellation"),
                    _ = tokio::time::sleep(std::time::Duration::from_millis(100)) => Ok(serde_json::json!({"cancelled": true})),
                }
            }
            _ => panic!("unknown fixture method"),
        };
        let response = match result {
            Ok(result) => serde_json::json!({"result": result}),
            Err(error) => serde_json::json!({"error": error}),
        };
        println!("{response}");
        std::io::stdout().flush().expect("fixture output");
    }
    core.shutdown().await.expect("fixture worker shutdown");
    uni_switch_lib::model_registry::shutdown();
}
