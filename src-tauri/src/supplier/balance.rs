use super::{get_json_with_user, now, parse_balance};
use crate::error::{AppError, Result};
use crate::types::{
    validate_balance, validate_url, BalanceAdapter, BalanceQuery, BalanceResult, BalanceWindow,
};
use serde_json::Value;

/// Keep a reverse proxy's deployment prefix, stripping only known inference suffixes.
pub fn balance_site_url(base_url: &str, query: &BalanceQuery) -> Result<url::Url> {
    let mut url = validate_url(query.site_url.as_deref().unwrap_or(base_url))?;
    let path = url.path().trim_end_matches('/');
    let prefix = if query.site_url.is_some() {
        path
    } else {
        path.strip_suffix("/backend-api/codex")
            .or_else(|| path.strip_suffix("/v1"))
            .unwrap_or(path)
    };
    let path = format!("{}/", prefix.trim_end_matches('/'));
    url.set_path(&path);
    Ok(url)
}

fn invalid(message: &str) -> AppError {
    AppError::new("balance_response", message)
}
pub(super) fn numeric(value: &Value) -> Option<f64> {
    value
        .as_f64()
        .or_else(|| value.as_str().and_then(|s| s.parse().ok()))
        .filter(|n| n.is_finite())
}
pub(super) fn number(value: &Value, field: &str) -> Result<f64> {
    value
        .get(field)
        .and_then(numeric)
        .ok_or_else(|| invalid("接口缺少有效的余额或额度数值，请检查供应商接口"))
}
fn text(value: &Value, field: &str) -> Option<String> {
    value
        .get(field)
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty())
        .map(|s| s.chars().filter(|c| !c.is_control()).take(120).collect())
}
fn timestamp(value: &Value, field: &str) -> Option<String> {
    text(value, field).or_else(|| {
        value
            .get(field)
            .and_then(Value::as_i64)
            .filter(|v| *v > 0)
            .map(|v| v.to_string())
    })
}
fn window(
    label: String,
    value: &Value,
    used: &str,
    total: &str,
    reset: &str,
) -> Result<Option<BalanceWindow>> {
    let Some(limit) = value.get(total).and_then(numeric).filter(|v| *v > 0.0) else {
        return Ok(None);
    };
    let used = number(value, used)?;
    Ok(Some(BalanceWindow {
        label,
        remaining: (limit - used).max(0.0),
        used,
        total: limit,
        reset_at: timestamp(value, reset),
    }))
}

pub(super) fn parse_adapter_balance(value: &Value, query: &BalanceQuery) -> Result<BalanceResult> {
    validate_balance(query)?;
    if value.get("success").and_then(Value::as_bool) == Some(false)
        || value.get("code").and_then(Value::as_bool) == Some(false)
        || value.get("isValid").and_then(Value::as_bool) == Some(false)
        || value.get("is_active").and_then(Value::as_bool) == Some(false)
        || value.get("error").is_some_and(|v| !v.is_null())
    {
        return Err(invalid(if query.adapter == BalanceAdapter::NewapiAccount {
            "New API 账户查询失败，请检查控制台 Access Token、用户 ID 和账户访问权限"
        } else {
            "供应商返回查询失败，请检查密钥和余额查询权限"
        }));
    }
    let mut result = BalanceResult {
        amount: None,
        unit: query.unit.clone(),
        checked_at: now(),
        scope: "余额".into(),
        unlimited: false,
        used: None,
        total: None,
        plan_name: None,
        expires_at: None,
        windows: Vec::new(),
        provider_type: None,
        note: None,
    };
    match query.adapter {
        BalanceAdapter::Auto => return Err(invalid("自动探测需要查询供应商接口")),
        BalanceAdapter::Custom => result.amount = Some(parse_balance(value, query)?),
        BalanceAdapter::Credit => result.amount = Some(number(value, "total_available")?),
        BalanceAdapter::NewapiAccount | BalanceAdapter::NewapiToken => {
            let data = value
                .get("data")
                .ok_or_else(|| invalid("New API 接口未返回 data，请检查站点地址和查询类型"))?;
            let account = query.adapter == BalanceAdapter::NewapiAccount;
            result.scope = if account {
                "账户余额"
            } else {
                "密钥额度"
            }
            .into();
            result.unlimited =
                !account && data.get("unlimited_quota").and_then(Value::as_bool) == Some(true);
            result.used = Some(
                number(data, if account { "used_quota" } else { "total_used" })? / query.divisor,
            );
            if !result.unlimited {
                let amount = number(data, if account { "quota" } else { "total_available" })?
                    / query.divisor;
                result.amount = Some(amount);
                result.total = Some(if account {
                    amount + result.used.unwrap_or(0.0)
                } else {
                    number(data, "total_granted")? / query.divisor
                });
            }
            result.plan_name = text(data, if account { "group" } else { "name" });
            result.expires_at = timestamp(data, "expires_at");
        }
        BalanceAdapter::Sub2api => {
            result.unit = "USD".into();
            result.plan_name = text(value, "planName");
            result.expires_at = timestamp(value, "expires_at");
            if let Some(quota) = value.get("quota").filter(|v| v.is_object()) {
                result.scope = "密钥额度".into();
                result.amount = Some(number(quota, "remaining")?);
                result.used = Some(number(quota, "used")?);
                result.total = Some(number(quota, "limit")?);
            } else if let Some(balance) = value.get("balance").and_then(numeric) {
                result.scope = "账户余额".into();
                result.amount = Some(balance);
            } else if let Some(sub) = value.get("subscription").filter(|v| v.is_object()) {
                result.scope = "订阅额度".into();
                result.expires_at = timestamp(sub, "expires_at");
                for (prefix, label) in [("daily", "每日"), ("weekly", "每周"), ("monthly", "每月")]
                {
                    if let Some(w) = window(
                        label.into(),
                        sub,
                        &format!("{prefix}_usage_usd"),
                        &format!("{prefix}_limit_usd"),
                        &format!("{prefix}_reset_at"),
                    )? {
                        result.windows.push(w);
                    }
                }
                if result.windows.is_empty() {
                    // Sub2API explicitly uses -1 to signal an unlimited subscription.
                    result.unlimited = value.get("remaining").and_then(numeric) == Some(-1.0);
                    if !result.unlimited {
                        result.amount = Some(number(value, "remaining")?);
                    }
                } else {
                    result.amount = result.windows.iter().map(|w| w.remaining).reduce(f64::min);
                }
            } else if value.get("mode").and_then(Value::as_str) != Some("quota_limited") {
                // Older Sub2API versions expose wallet/subscription remaining at the root.
                let amount = number(value, "remaining")?;
                result.unlimited = amount == -1.0;
                result.scope = if result.plan_name.as_deref() == Some("钱包余额") {
                    "账户余额"
                } else {
                    "可用额度"
                }
                .into();
                result.amount = if result.unlimited { None } else { Some(amount) };
            }
            if let Some(windows) = value.get("rate_limits").and_then(Value::as_array) {
                for v in windows.iter().take(20) {
                    let label = match v.get("window").and_then(Value::as_str) {
                        Some("5h") => "5 小时额度",
                        Some("1d") => "每日额度",
                        Some("7d") => "每周额度",
                        _ => "周期额度",
                    };
                    if let Some(w) = window(label.into(), v, "used", "limit", "reset_at")? {
                        result.windows.push(w);
                    }
                }
                if result.amount.is_none() && !result.windows.is_empty() && !result.unlimited {
                    result.scope = "周期额度".into();
                    result.amount = result.windows.iter().map(|w| w.remaining).reduce(f64::min);
                }
            }
            if result.amount.is_none() && !result.unlimited {
                return Err(invalid(
                    "Sub2API 未返回余额或订阅额度，请检查站点版本和订阅状态",
                ));
            }
        }
    }
    if [result.amount, result.used, result.total]
        .iter()
        .flatten()
        .any(|v| !v.is_finite())
    {
        return Err(invalid("余额换算结果无效"));
    }
    Ok(result)
}

pub async fn query_balance(
    base_url: &str,
    key: &str,
    query: BalanceQuery,
) -> Result<BalanceResult> {
    validate_balance(&query)?;
    let site = balance_site_url(base_url, &query)?;
    let path = match query.adapter {
        BalanceAdapter::Auto => return super::query_auto_balance(base_url, key, None, None).await,
        BalanceAdapter::Sub2api => "v1/usage",
        BalanceAdapter::NewapiAccount => "api/user/self",
        // Gin registers GET("/") for this endpoint and redirects the version without /.
        BalanceAdapter::NewapiToken => "api/usage/token/",
        BalanceAdapter::Credit => "v1/dashboard/billing/credit_grants",
        BalanceAdapter::Custom => &query.path,
    };
    let url = site.join(path).map_err(|_| invalid("余额接口路径无效"))?;
    if url.origin() != site.origin() {
        return Err(invalid("余额接口路径不能指向其他站点"));
    }
    let user_id = if query.adapter == BalanceAdapter::NewapiAccount {
        query.user_id.as_deref()
    } else {
        None
    };
    let value = get_json_with_user(url, key, user_id).await.map_err(|e| {
        if query.adapter == BalanceAdapter::NewapiAccount && e.code == "supplier_http" {
            AppError::new(
                &e.code,
                format!(
                    "{}。此查询需要控制台 Access Token 和用户 ID，API Key 无法替代。",
                    e.message
                ),
            )
        } else {
            e
        }
    })?;
    parse_adapter_balance(&value, &query)
}
