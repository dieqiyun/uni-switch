use super::*;
use serde_json::json;

fn release(tag: &str) -> Vec<u8> {
    serde_json::to_vec(&json!({
        "tag_name":tag,"html_url":format!("https://github.com/example/uni-switch/releases/tag/{tag}"),
        "draft":false,"prerelease":false,"body":"更新说明","published_at":"2026-10-07T00:00:00Z",
        "assets":[{"name":"uni-switch_0.5.17_x64-setup.exe",
          "browser_download_url":format!("https://github.com/example/uni-switch/releases/download/{tag}/uni-switch_0.5.17_x64-setup.exe")}]
    })).unwrap()
}

#[test]
fn versions_compare_numbers_and_never_downgrade() {
    assert!(
        parse_release(&release("v0.5.17"), "example/uni-switch", "0.5.16")
            .unwrap()
            .available
    );
    assert!(
        !parse_release(&release("v0.5.17"), "example/uni-switch", "0.5.17")
            .unwrap()
            .available
    );
    assert!(
        !parse_release(&release("v0.5.17"), "example/uni-switch", "0.6.0")
            .unwrap()
            .available
    );
    assert!(
        parse_release(&release("v0.10.0"), "example/uni-switch", "0.9.9")
            .unwrap()
            .available
    );
    for invalid in [
        "v1.0",
        "1.0.0-rc.1",
        "01.0.0",
        "1.0.0/a",
        "1.0.0?x",
        "999999999999999999999999.0.0",
    ] {
        assert!(version(invalid).is_err());
    }
}

#[test]
fn rejects_foreign_urls_and_preview_releases() {
    let mut value: serde_json::Value = serde_json::from_slice(&release("v0.5.17")).unwrap();
    for field in ["draft", "prerelease"] {
        value[field] = json!(true);
        assert_eq!(
            parse_release(
                &serde_json::to_vec(&value).unwrap(),
                "example/uni-switch",
                "0.5.16"
            )
            .unwrap_err()
            .code,
            "update_unpublished"
        );
        value[field] = json!(false);
    }
    value["html_url"] = json!("https://github.com/other/app/releases/tag/v0.5.17");
    assert_eq!(
        parse_release(
            &serde_json::to_vec(&value).unwrap(),
            "example/uni-switch",
            "0.5.16"
        )
        .unwrap_err()
        .code,
        "update_response"
    );
    assert!(!trusted_url(
        "https://user@github.com/example/uni-switch/releases",
        "https://user@github.com/example/uni-switch/releases"
    ));
    assert!(!valid_repository("example/../repo"));
}

#[test]
fn download_matches_version_and_notes_are_bounded() {
    let result = parse_release(&release("v0.5.17"), "example/uni-switch", "0.5.16").unwrap();
    assert!(result.download_url.is_some());
    assert!(
        parse_release(&release("v0.5.17"), "Example/Uni-Switch", "0.5.16")
            .unwrap()
            .available
    );
    let mut value: serde_json::Value = serde_json::from_slice(&release("v0.5.17")).unwrap();
    value["assets"][0]["browser_download_url"] = json!("https://evil.test/installer.exe");
    value["body"] = json!("中".repeat(7000));
    let result = parse_release(
        &serde_json::to_vec(&value).unwrap(),
        "example/uni-switch",
        "0.5.16",
    )
    .unwrap();
    assert!(result.download_url.is_none());
    assert_eq!(result.notes.chars().count(), 6000);
}

#[tokio::test]
async fn request_has_no_supplier_auth_and_reports_github_errors() {
    use axum::{http::HeaderMap, routing::get, Router};
    let app = Router::new()
        .route(
            "/latest",
            get(|headers: HeaderMap| async move {
                assert!(headers.get("authorization").is_none());
                assert!(headers.get("x-api-key").is_none());
                assert_eq!(headers.get("user-agent").unwrap(), "uni-switch/0.5.16");
                release("v0.5.17")
            }),
        )
        .route(
            "/missing",
            get(|| async { axum::http::StatusCode::NOT_FOUND }),
        )
        .route(
            "/rate",
            get(|| async { axum::http::StatusCode::TOO_MANY_REQUESTS }),
        );
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let origin = format!("http://{}", listener.local_addr().unwrap());
    let task = tokio::spawn(async move { axum::serve(listener, app).await.unwrap() });
    assert!(
        fetch_release(&format!("{origin}/latest"), "example/uni-switch", "0.5.16")
            .await
            .unwrap()
            .available
    );
    assert_eq!(
        fetch_release(&format!("{origin}/missing"), "example/uni-switch", "0.5.16")
            .await
            .unwrap_err()
            .code,
        "update_unpublished"
    );
    assert_eq!(
        fetch_release(&format!("{origin}/rate"), "example/uni-switch", "0.5.16")
            .await
            .unwrap_err()
            .code,
        "update_rate_limit"
    );
    task.abort();
}
