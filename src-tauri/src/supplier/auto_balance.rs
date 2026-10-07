use super::balance::{number, numeric, parse_adapter_balance};
use super::{balance_site_url, now, read_json, supplier_client};
use crate::error::{AppError, Result};
use crate::types::{BalanceAdapter, BalanceQuery, BalanceResult};
use serde_json::Value;
use std::time::{Duration, Instant};
use std::{
    collections::HashMap,
    sync::{Mutex, OnceLock},
};

#[derive(Clone, Copy)]
enum Strategy {
    Sub2api,
    Token,
    Billing { newapi: bool },
    Credit,
}
static STRATEGIES: OnceLock<Mutex<HashMap<String, (Strategy, Instant)>>> = OnceLock::new();

fn strategy_key(base: &str, key: &str) -> String {
    use sha2::{Digest, Sha256};
    format!("{:x}", Sha256::digest(format!("{base}\0{key}").as_bytes()))
}

struct Probe {
    client: reqwest::Client,
    site: url::Url,
    deadline: Instant,
    failures: Vec<AppError>,
}
impl Probe {
    async fn get(&mut self, path: &str, key: Option<&str>, user: Option<&str>) -> Result<Value> {
        let remaining = self.deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return Err(AppError::new("balance_timeout", "自动查询超时，请稍后刷新"));
        }
        let url = self
            .site
            .join(path)
            .map_err(|_| AppError::new("balance_path", "余额接口地址无效"))?;
        if url.origin() != self.site.origin() {
            return Err(AppError::new("balance_path", "余额接口不能指向其他站点"));
        }
        let result = read_json(
            &self.client,
            url,
            key,
            user,
            remaining.min(Duration::from_secs(4)),
        )
        .await;
        if let Err(e) = &result {
            self.failures.push(AppError::new(&e.code, &e.message));
        }
        result
    }
    fn limited(&self) -> bool {
        self.failures.iter().any(|e| e.message.contains("HTTP 429"))
    }
    fn error(&self) -> AppError {
        if let Some(e) = self
            .failures
            .iter()
            .find(|e| e.message.contains("HTTP 429"))
        {
            return AppError::new(&e.code, &e.message);
        }
        if Instant::now() >= self.deadline {
            return AppError::new("balance_timeout", "自动查询超时，请检查网络后刷新");
        }
        if self
            .failures
            .iter()
            .any(|e| e.message.contains("HTTP 401") || e.message.contains("HTTP 403"))
        {
            return AppError::new(
                "balance_auth",
                "余额接口拒绝访问，请检查 API Key、IP 限制或查询权限",
            );
        }
        if self.failures.iter().any(|e| e.code == "network") {
            return AppError::new("network", "无法连接余额接口，请检查供应商地址和网络后刷新");
        }
        AppError::new(
            "balance_unsupported",
            "未找到可用的余额接口；供应商可能未开放 API Key 查询",
        )
    }
}
fn successful(v: &Value) -> bool {
    v.is_object()
        && v.get("success").and_then(Value::as_bool) != Some(false)
        && v.get("code").and_then(Value::as_bool) != Some(false)
        && !v.get("error").is_some_and(|v| !v.is_null())
}
fn sub2api_shape(v: &Value) -> bool {
    successful(v)
        && (matches!(
            v.get("mode").and_then(Value::as_str),
            Some("unrestricted" | "quota_limited")
        ) || (v.get("isValid").is_some() || v.get("is_active").is_some())
            && (v.get("remaining").is_some()
                || v.get("quota").is_some()
                || v.get("balance").is_some()))
}
fn token_shape(v: &Value) -> bool {
    successful(v)
        && v.get("data").is_some_and(|d| {
            d.get("object").and_then(Value::as_str) == Some("token_usage")
                || d.get("total_available").is_some()
                    && d.get("total_used").is_some()
                    && d.get("unlimited_quota").is_some()
        })
}
fn auto_query(adapter: BalanceAdapter) -> BalanceQuery {
    BalanceQuery {
        adapter,
        unit: "USD".into(),
        divisor: 500_000.0,
        ..BalanceQuery::default()
    }
}

fn status_settings(status: Option<&Value>) -> (f64, String, f64) {
    let data = status.filter(|s| successful(s)).and_then(|s| s.get("data"));
    let divisor = data
        .and_then(|d| d.get("quota_per_unit"))
        .and_then(numeric)
        .filter(|n| *n > 0.0)
        .unwrap_or(500_000.0);
    match data
        .and_then(|d| d.get("quota_display_type"))
        .and_then(Value::as_str)
    {
        Some("TOKENS") => (1.0, "tokens".into(), 1.0),
        Some("CNY") => match data
            .and_then(|d| d.get("usd_exchange_rate"))
            .and_then(numeric)
            .filter(|v| *v > 0.0)
        {
            Some(rate) => (divisor, "CNY".into(), rate),
            None => (divisor, "USD".into(), 1.0),
        },
        _ => (divisor, "USD".into(), 1.0),
    }
}
fn token_result(
    value: &Value,
    status: Option<&Value>,
    legacy: Option<&BalanceQuery>,
) -> Result<BalanceResult> {
    let (divisor, unit, rate) = status_settings(status);
    // Keep an existing explicit conversion when status is unavailable.
    let mut q = auto_query(BalanceAdapter::NewapiToken);
    q.divisor = divisor;
    q.unit = unit;
    if status.is_none() {
        if let Some(old) = legacy.filter(|q| q.adapter == BalanceAdapter::NewapiToken) {
            q.divisor = old.divisor;
            q.unit.clone_from(&old.unit);
        }
    }
    let mut result = parse_adapter_balance(value, &q)?;
    for v in [&mut result.amount, &mut result.used, &mut result.total]
        .into_iter()
        .flatten()
    {
        *v *= rate;
        if !v.is_finite() {
            return Err(AppError::new("balance_response", "站点额度换算无效"));
        }
    }
    result.provider_type = Some("New API".into());
    Ok(result)
}

pub(super) fn parse_billing(
    subscription: &Value,
    usage: &Value,
    status: Option<&Value>,
    token: Option<&BalanceResult>,
) -> Result<BalanceResult> {
    if !successful(subscription) || !successful(usage) {
        return Err(AppError::new(
            "balance_response",
            "站点计费接口返回查询失败",
        ));
    }
    if subscription.get("object").and_then(Value::as_str) != Some("billing_subscription") {
        return Err(AppError::new("balance_response", "站点计费接口格式不匹配"));
    }
    let total = number(subscription, "hard_limit_usd")?;
    let used = number(usage, "total_usage")? / 100.0;
    let unit = match status
        .and_then(|s| s.get("data"))
        .and_then(|d| d.get("quota_display_type"))
        .and_then(Value::as_str)
    {
        Some("CNY") => "CNY",
        Some("TOKENS") => "tokens",
        _ => "USD",
    }
    .to_owned();
    let unlimited = token.is_some_and(|t| t.unlimited) && total == 100_000_000.0;
    let amount = total - used;
    if !amount.is_finite() {
        return Err(AppError::new("balance_response", "站点额度无效"));
    }
    Ok(BalanceResult {
        amount: if unlimited { None } else { Some(amount) },
        unit,
        checked_at: now(),
        scope: "可用额度".into(),
        unlimited,
        used: Some(used),
        total: if unlimited { None } else { Some(total) },
        plan_name: None,
        expires_at: subscription
            .get("access_until")
            .and_then(Value::as_i64)
            .filter(|v| *v > 0)
            .map(|v| v.to_string()),
        windows: Vec::new(),
        provider_type: Some(
            if token.is_some() {
                "New API"
            } else {
                "兼容计费接口"
            }
            .into(),
        ),
        note: Some("由站点决定返回账户余额或密钥额度".into()),
    })
}

pub async fn query_auto_balance(
    base_url: &str,
    key: &str,
    legacy: Option<BalanceQuery>,
    account_token: Option<&str>,
) -> Result<BalanceResult> {
    let fingerprint = strategy_key(base_url, key);
    let strategies = STRATEGIES.get_or_init(|| Mutex::new(HashMap::new()));
    let cached = strategies
        .lock()
        .ok()
        .and_then(|cache| cache.get(&fingerprint).copied())
        .filter(|(_, until)| *until > Instant::now());
    if account_token.is_none() {
        if let Some((strategy, _)) = cached {
            let q = legacy
                .clone()
                .unwrap_or_else(|| auto_query(BalanceAdapter::Auto));
            let mut probe = Probe {
                client: supplier_client()?,
                site: balance_site_url(base_url, &q)?,
                deadline: Instant::now() + Duration::from_secs(12),
                failures: Vec::new(),
            };
            let result = match strategy {
                Strategy::Sub2api => {
                    probe
                        .get("v1/usage", Some(key), None)
                        .await
                        .and_then(|value| {
                            if !sub2api_shape(&value) {
                                return Err(AppError::new("balance_response", "余额接口已变化"));
                            }
                            let mut result = parse_adapter_balance(
                                &value,
                                &auto_query(BalanceAdapter::Sub2api),
                            )?;
                            result.provider_type = Some("Sub2API".into());
                            Ok(result)
                        })
                }
                Strategy::Token => {
                    let token = probe.get("api/usage/token/", Some(key), None).await;
                    let status = if probe.limited() {
                        None
                    } else {
                        probe
                            .get("api/status", None, None)
                            .await
                            .ok()
                            .filter(successful)
                    };
                    token
                        .and_then(|v| token_result(&v, status.as_ref(), legacy.as_ref()))
                        .map(|mut r| {
                            r.note = Some("当前为密钥额度；密钥不限额不代表账户余额无限".into());
                            r
                        })
                }
                Strategy::Billing { newapi } => {
                    let sub = probe
                        .get("v1/dashboard/billing/subscription", Some(key), None)
                        .await;
                    let usage = if probe.limited() {
                        Err(probe.error())
                    } else {
                        probe
                            .get("v1/dashboard/billing/usage", Some(key), None)
                            .await
                    };
                    let status = if probe.limited() {
                        None
                    } else {
                        probe
                            .get("api/status", None, None)
                            .await
                            .ok()
                            .filter(successful)
                    };
                    let token = if newapi && !probe.limited() {
                        probe
                            .get("api/usage/token/", Some(key), None)
                            .await
                            .ok()
                            .and_then(|v| token_result(&v, status.as_ref(), legacy.as_ref()).ok())
                    } else {
                        None
                    };
                    match (sub, usage) {
                        (Ok(s), Ok(u)) => parse_billing(&s, &u, status.as_ref(), token.as_ref()),
                        (Err(e), _) | (_, Err(e)) => Err(e),
                    }
                }
                Strategy::Credit => probe
                    .get("v1/dashboard/billing/credit_grants", Some(key), None)
                    .await
                    .and_then(|v| parse_adapter_balance(&v, &auto_query(BalanceAdapter::Credit)))
                    .map(|mut r| {
                        r.provider_type = Some("兼容余额接口".into());
                        r
                    }),
            };
            if probe.limited() {
                return Err(probe.error());
            }
            if let Ok(result) = result {
                return Ok(result);
            }
            if let Ok(mut cache) = strategies.lock() {
                cache.remove(&fingerprint);
            }
        }
    }
    let result = probe_auto_balance(base_url, key, legacy, account_token).await?;
    let strategy = match (result.provider_type.as_deref(), result.scope.as_str()) {
        (Some("Sub2API"), _) => Some(Strategy::Sub2api),
        (Some("New API"), "密钥额度") => Some(Strategy::Token),
        (Some("New API" | "兼容计费接口"), "可用额度") if !result.unlimited => {
            Some(Strategy::Billing {
                newapi: result.provider_type.as_deref() == Some("New API"),
            })
        }
        (Some("兼容余额接口"), _) => Some(Strategy::Credit),
        _ => None,
    };
    if let Some(strategy) = strategy {
        if let Ok(mut cache) = strategies.lock() {
            cache.retain(|_, (_, until)| *until > Instant::now());
            if cache.len() >= 100 {
                cache.clear();
            }
            cache.insert(
                fingerprint,
                (strategy, Instant::now() + Duration::from_secs(900)),
            );
        }
    }
    Ok(result)
}

async fn probe_auto_balance(
    base_url: &str,
    key: &str,
    legacy: Option<BalanceQuery>,
    account_token: Option<&str>,
) -> Result<BalanceResult> {
    let query = legacy
        .clone()
        .unwrap_or_else(|| auto_query(BalanceAdapter::Auto));
    let mut probe = Probe {
        client: supplier_client()?,
        site: balance_site_url(base_url, &query)?,
        deadline: Instant::now() + Duration::from_secs(22),
        failures: Vec::new(),
    };
    if let Ok(value) = probe.get("v1/usage", Some(key), None).await {
        if sub2api_shape(&value) {
            let mut result = parse_adapter_balance(&value, &auto_query(BalanceAdapter::Sub2api))?;
            result.provider_type = Some("Sub2API".into());
            return Ok(result);
        }
    }
    if probe.limited() {
        return Err(probe.error());
    }
    let raw_token = probe
        .get("api/usage/token/", Some(key), None)
        .await
        .ok()
        .filter(token_shape);
    if probe.limited() {
        return Err(probe.error());
    }
    // Site metadata is public: never attach either API or console credentials.
    let status = probe
        .get("api/status", None, None)
        .await
        .ok()
        .filter(successful);
    if probe.limited() {
        return Err(probe.error());
    }
    let token = raw_token
        .as_ref()
        .map(|v| token_result(v, status.as_ref(), legacy.as_ref()))
        .transpose()?;

    if let (Some(saved), Some(access)) = (
        legacy
            .as_ref()
            .filter(|q| q.adapter == BalanceAdapter::NewapiAccount),
        account_token,
    ) {
        if let Ok(value) = probe
            .get("api/user/self", Some(access), saved.user_id.as_deref())
            .await
        {
            if let Ok(mut result) = parse_adapter_balance(&value, saved) {
                result.provider_type = Some("New API".into());
                return Ok(result);
            }
        }
    }
    if probe.limited() {
        return Err(probe.error());
    }
    if let Ok(subscription) = probe
        .get("v1/dashboard/billing/subscription", Some(key), None)
        .await
    {
        if successful(&subscription)
            && subscription.get("object").and_then(Value::as_str) == Some("billing_subscription")
        {
            if let Ok(usage) = probe
                .get("v1/dashboard/billing/usage", Some(key), None)
                .await
            {
                if let Ok(result) =
                    parse_billing(&subscription, &usage, status.as_ref(), token.as_ref())
                {
                    return Ok(result);
                }
            }
        }
    }
    if let Some(mut token) = token {
        token.note = Some("当前为密钥额度；账户余额接口未返回可用结果".into());
        return Ok(token);
    }
    if probe.limited() {
        return Err(probe.error());
    }
    if let Ok(value) = probe
        .get("v1/dashboard/billing/credit_grants", Some(key), None)
        .await
    {
        if successful(&value) && value.get("total_available").and_then(numeric).is_some() {
            let mut result = parse_adapter_balance(&value, &auto_query(BalanceAdapter::Credit))?;
            result.provider_type = Some("兼容余额接口".into());
            return Ok(result);
        }
    }
    if probe.limited() {
        return Err(probe.error());
    }
    if let Some(saved) = legacy.filter(|q| q.adapter == BalanceAdapter::Custom) {
        if let Ok(value) = probe.get(&saved.path, Some(key), None).await {
            if let Ok(mut result) = parse_adapter_balance(&value, &saved) {
                result.provider_type = Some("已保存的查询接口".into());
                return Ok(result);
            }
        }
    }
    Err(probe.error())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::io::{Read, Write};
    fn server(
        routes: Vec<(&'static str, &'static str, &'static str)>,
    ) -> (String, std::thread::JoinHandle<Vec<String>>) {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        listener.set_nonblocking(true).unwrap();
        let url = format!("http://{}/gateway/v1", listener.local_addr().unwrap());
        let thread = std::thread::spawn(move || {
            let end = Instant::now() + Duration::from_secs(6);
            let mut requests = Vec::new();
            for (path, status, body) in routes {
                let (mut stream, _) = loop {
                    match listener.accept() {
                        Ok(v) => break v,
                        Err(e)
                            if e.kind() == std::io::ErrorKind::WouldBlock
                                && Instant::now() < end =>
                        {
                            std::thread::sleep(Duration::from_millis(2))
                        }
                        other => panic!("missing request {path}: {other:?}"),
                    }
                };
                stream.set_nonblocking(false).unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(2)))
                    .unwrap();
                let mut bytes = [0; 8192];
                let n = stream.read(&mut bytes).unwrap();
                let request = String::from_utf8(bytes[..n].to_vec()).unwrap();
                assert!(
                    request.starts_with(&format!("GET {path} ")),
                    "unexpected request: {request}"
                );
                let response = format!("HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len());
                stream.write_all(response.as_bytes()).unwrap();
                requests.push(request.to_lowercase());
            }
            requests
        });
        (url, thread)
    }
    #[tokio::test]
    async fn automatically_detects_sub2api_without_a_saved_query() {
        let (url, s) = server(vec![(
            "/gateway/v1/usage",
            "200 OK",
            r#"{"mode":"unrestricted","isValid":true,"balance":0,"unit":"USD"}"#,
        )]);
        let result = query_auto_balance(&url, "test-key", None, None)
            .await
            .unwrap();
        assert_eq!(result.amount, Some(0.0));
        assert_eq!(result.provider_type.as_deref(), Some("Sub2API"));
        assert!(s.join().unwrap()[0].contains("authorization: bearer test-key"));
    }
    #[tokio::test]
    async fn automatically_combines_newapi_billing_and_fetches_status_without_credentials() {
        let (url, s) = server(vec![
            ("/gateway/v1/usage", "404 Not Found", "{}"),
            (
                "/gateway/api/usage/token/",
                "200 OK",
                r#"{"code":true,"data":{"object":"token_usage","total_available":12500000,"total_used":0,"total_granted":12500000,"unlimited_quota":false}}"#,
            ),
            (
                "/gateway/api/status",
                "200 OK",
                r#"{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"USD"}}"#,
            ),
            (
                "/gateway/v1/dashboard/billing/subscription",
                "200 OK",
                r#"{"object":"billing_subscription","hard_limit_usd":120}"#,
            ),
            (
                "/gateway/v1/dashboard/billing/usage",
                "200 OK",
                r#"{"object":"list","total_usage":2000}"#,
            ),
        ]);
        let result = query_auto_balance(&url, "test-key", None, None)
            .await
            .unwrap();
        assert_eq!(result.amount, Some(100.0));
        assert_eq!(result.scope, "可用额度");
        assert_eq!(result.provider_type.as_deref(), Some("New API"));
        let requests = s.join().unwrap();
        assert!(!requests[2].contains("authorization"));
        assert!(requests
            .iter()
            .enumerate()
            .filter(|(i, _)| *i != 2)
            .all(|(_, r)| r.contains("authorization: bearer test-key")));
    }
    #[tokio::test]
    async fn billing_failure_returns_zero_key_quota_with_explicit_scope() {
        let (url, s) = server(vec![
            ("/gateway/v1/usage", "404 Not Found", "{}"),
            (
                "/gateway/api/usage/token/",
                "200 OK",
                r#"{"code":true,"data":{"object":"token_usage","total_available":0,"total_used":500000,"total_granted":500000,"unlimited_quota":false}}"#,
            ),
            ("/gateway/api/status", "404 Not Found", "{}"),
            (
                "/gateway/v1/dashboard/billing/subscription",
                "401 Unauthorized",
                r#"{"error":"private-key"}"#,
            ),
        ]);
        let result = query_auto_balance(&url, "test-key", None, None)
            .await
            .unwrap();
        assert_eq!(result.amount, Some(0.0));
        assert_eq!(result.scope, "密钥额度");
        assert!(result.note.unwrap().contains("账户余额接口未返回"));
        s.join().unwrap();
    }
    #[tokio::test]
    async fn subsequent_token_queries_use_known_endpoint_and_keep_scope_and_units() {
        let token = r#"{"code":true,"data":{"object":"token_usage","total_available":1000000,"total_used":500000,"total_granted":1500000,"unlimited_quota":false}}"#;
        let status = r#"{"success":true,"data":{"quota_per_unit":1000000,"quota_display_type":"CNY","usd_exchange_rate":7}}"#;
        let (url, s) = server(vec![
            ("/gateway/v1/usage", "404 Not Found", "{}"),
            ("/gateway/api/usage/token/", "200 OK", token),
            ("/gateway/api/status", "200 OK", status),
            (
                "/gateway/v1/dashboard/billing/subscription",
                "404 Not Found",
                "{}",
            ),
            ("/gateway/api/usage/token/", "200 OK", token),
            ("/gateway/api/status", "200 OK", status),
        ]);
        for _ in 0..2 {
            let result = query_auto_balance(&url, "cached-token-test-key", None, None)
                .await
                .unwrap();
            assert_eq!(result.amount, Some(7.0));
            assert_eq!(result.scope, "密钥额度");
            assert_eq!(result.unit, "CNY");
        }
        let requests = s.join().unwrap();
        assert!(!requests[5].contains("authorization"));
    }
    #[tokio::test]
    async fn legacy_custom_query_is_kept_after_automatic_probes_fail() {
        let (url, s) = server(vec![
            ("/gateway/v1/usage", "200 OK", r#"{"remaining":999}"#),
            ("/gateway/api/usage/token/", "404 Not Found", "{}"),
            ("/gateway/api/status", "404 Not Found", "{}"),
            (
                "/gateway/v1/dashboard/billing/subscription",
                "404 Not Found",
                "{}",
            ),
            (
                "/gateway/v1/dashboard/billing/credit_grants",
                "404 Not Found",
                "{}",
            ),
            ("/custom", "200 OK", r#"{"data":{"balance":"4.5"}}"#),
        ]);
        let q = BalanceQuery {
            path: "/custom".into(),
            json_path: "data.balance".into(),
            ..BalanceQuery::default()
        };
        let result = query_auto_balance(&url, "test-key", Some(q), None)
            .await
            .unwrap();
        assert_eq!(result.amount, Some(4.5));
        assert_eq!(result.provider_type.as_deref(), Some("已保存的查询接口"));
        s.join().unwrap();
    }
    #[tokio::test]
    async fn rate_limit_stops_probing_and_unsupported_is_not_zero_balance() {
        let (url, s) = server(vec![(
            "/gateway/v1/usage",
            "429 Too Many Requests",
            r#"{"error":"private-key"}"#,
        )]);
        let err = query_auto_balance(&url, "test-key", None, None)
            .await
            .unwrap_err();
        assert!(err.message.contains("429"));
        assert!(!err.message.contains("private-key"));
        s.join().unwrap();
        let (url, s) = server(vec![
            ("/gateway/v1/usage", "404 Not Found", "{}"),
            ("/gateway/api/usage/token/", "404 Not Found", "{}"),
            ("/gateway/api/status", "404 Not Found", "{}"),
            (
                "/gateway/v1/dashboard/billing/subscription",
                "404 Not Found",
                "{}",
            ),
            (
                "/gateway/v1/dashboard/billing/credit_grants",
                "200 OK",
                r#"{"success":false,"total_available":0}"#,
            ),
        ]);
        assert_eq!(
            query_auto_balance(&url, "test-key", None, None)
                .await
                .unwrap_err()
                .code,
            "balance_unsupported"
        );
        s.join().unwrap();
    }
    #[test]
    fn billing_calculates_remaining_and_preserves_server_units_without_claiming_account_scope() {
        let sub = json!({"object":"billing_subscription","hard_limit_usd":120,"access_until":0});
        let usage = json!({"object":"list","total_usage":2000});
        let result = parse_billing(&sub, &usage, None, None).unwrap();
        assert_eq!(result.amount, Some(100.0));
        assert_eq!(result.scope, "可用额度");
        assert!(result.note.unwrap().contains("账户余额或密钥额度"));
        let result = parse_billing(
            &sub,
            &usage,
            Some(&json!({"success":true,"data":{"quota_display_type":"CNY"}})),
            None,
        )
        .unwrap();
        assert_eq!(result.unit, "CNY");
        assert_eq!(result.amount, Some(100.0));
        assert!(parse_billing(&json!({"hard_limit_usd":120}), &usage, None, None).is_err());
        assert!(parse_billing(&sub, &json!({"error":"denied"}), None, None).is_err());
    }
    #[test]
    fn token_conversion_reads_site_settings_and_does_not_invent_wallet_balance() {
        let token = json!({"code":true,"data":{"object":"token_usage","total_available":1000000,"total_used":500000,"total_granted":1500000,"unlimited_quota":false}});
        let status = json!({"success":true,"data":{"quota_per_unit":1000000,"quota_display_type":"CNY","usd_exchange_rate":7}});
        let result = token_result(&token, Some(&status), None).unwrap();
        assert_eq!(result.amount, Some(7.0));
        assert_eq!(result.used, Some(3.5));
        assert_eq!(result.unit, "CNY");
        let result = token_result(
            &token,
            Some(&json!({"data":{"quota_display_type":"TOKENS"}})),
            None,
        )
        .unwrap();
        assert_eq!(result.amount, Some(1000000.0));
        assert_eq!(result.scope, "密钥额度");
    }
}
