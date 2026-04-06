use super::*;
use crate::exec_output::StreamOutput;
use crate::protocol::RateLimitWindow;
use chrono::DateTime;
use chrono::Duration as ChronoDuration;
use chrono::TimeZone;
use chrono::Utc;
use http::Response as HttpResponse;
use pretty_assertions::assert_eq;
use reqwest::Response;
use reqwest::ResponseBuilderExt;
use reqwest::StatusCode;
use reqwest::Url;

fn rate_limit_snapshot() -> RateLimitSnapshot {
    let primary_reset_at = Utc
        .with_ymd_and_hms(2024, 1, 1, 1, 0, 0)
        .unwrap()
        .timestamp();
    let secondary_reset_at = Utc
        .with_ymd_and_hms(2024, 1, 1, 2, 0, 0)
        .unwrap()
        .timestamp();
    RateLimitSnapshot {
        limit_id: None,
        limit_name: None,
        primary: Some(RateLimitWindow {
            used_percent: 50.0,
            window_minutes: Some(60),
            resets_at: Some(primary_reset_at),
        }),
        secondary: Some(RateLimitWindow {
            used_percent: 30.0,
            window_minutes: Some(120),
            resets_at: Some(secondary_reset_at),
        }),
        credits: None,
        plan_type: None,
    }
}

fn with_now_override<T>(now: DateTime<Utc>, f: impl FnOnce() -> T) -> T {
    NOW_OVERRIDE.with(|cell| {
        *cell.borrow_mut() = Some(now);
        let result = f();
        *cell.borrow_mut() = None;
        result
    })
}

#[test]
fn usage_limit_reached_error_formats_plus_plan() {
    let err = UsageLimitReachedError {
        plan_type: Some(PlanType::Known(KnownPlan::Plus)),
        resets_at: None,
        rate_limits: Some(Box::new(rate_limit_snapshot())),
        promo_message: None,
    };
    assert_eq!(
        err.to_string(),
        "Вы достигли лимита использования. Обновите план до Pro (https://chatgpt.com/explore/pro), перейдите на https://chatgpt.com/codex/settings/usage, чтобы купить дополнительные кредиты или повторите позже."
    );
}

#[test]
fn server_overloaded_maps_to_protocol() {
    let err = CodexErr::ServerOverloaded;
    assert_eq!(
        err.to_codex_protocol_error(),
        CodexErrorInfo::ServerOverloaded
    );
}

#[test]
fn sandbox_denied_uses_aggregated_output_when_stderr_empty() {
    let output = ExecToolCallOutput {
        exit_code: 77,
        stdout: StreamOutput::new(String::new()),
        stderr: StreamOutput::new(String::new()),
        aggregated_output: StreamOutput::new("aggregate detail".to_string()),
        duration: Duration::from_millis(10),
        timed_out: false,
    };
    let err = CodexErr::Sandbox(SandboxErr::Denied {
        output: Box::new(output),
        network_policy_decision: None,
    });
    assert_eq!(get_error_message_ui(&err), "aggregate detail");
}

#[test]
fn sandbox_denied_reports_both_streams_when_available() {
    let output = ExecToolCallOutput {
        exit_code: 9,
        stdout: StreamOutput::new("stdout detail".to_string()),
        stderr: StreamOutput::new("stderr detail".to_string()),
        aggregated_output: StreamOutput::new(String::new()),
        duration: Duration::from_millis(10),
        timed_out: false,
    };
    let err = CodexErr::Sandbox(SandboxErr::Denied {
        output: Box::new(output),
        network_policy_decision: None,
    });
    assert_eq!(get_error_message_ui(&err), "stderr detail\nstdout detail");
}

#[test]
fn sandbox_denied_reports_stdout_when_no_stderr() {
    let output = ExecToolCallOutput {
        exit_code: 11,
        stdout: StreamOutput::new("stdout only".to_string()),
        stderr: StreamOutput::new(String::new()),
        aggregated_output: StreamOutput::new(String::new()),
        duration: Duration::from_millis(8),
        timed_out: false,
    };
    let err = CodexErr::Sandbox(SandboxErr::Denied {
        output: Box::new(output),
        network_policy_decision: None,
    });
    assert_eq!(get_error_message_ui(&err), "stdout only");
}

#[test]
fn to_error_event_handles_response_stream_failed() {
    let response = HttpResponse::builder()
        .status(StatusCode::TOO_MANY_REQUESTS)
        .url(Url::parse("http://example.com").unwrap())
        .body("")
        .unwrap();
    let source = Response::from(response).error_for_status_ref().unwrap_err();
    let err = CodexErr::ResponseStreamFailed(ResponseStreamFailed {
        source,
        request_id: Some("req-123".to_string()),
    });

    let event = err.to_error_event(Some("prefix".to_string()));

    assert_eq!(
        event.message,
        "prefix: Ошибка при чтении ответа сервера: HTTP status client error (429 Too Many Requests) for url (http://example.com/), id запроса: req-123"
    );
    assert_eq!(
        event.codex_error_info,
        Some(CodexErrorInfo::ResponseStreamConnectionFailed {
            http_status_code: Some(429)
        })
    );
}

#[test]
fn sandbox_denied_reports_exit_code_when_no_output_available() {
    let output = ExecToolCallOutput {
        exit_code: 13,
        stdout: StreamOutput::new(String::new()),
        stderr: StreamOutput::new(String::new()),
        aggregated_output: StreamOutput::new(String::new()),
        duration: Duration::from_millis(5),
        timed_out: false,
    };
    let err = CodexErr::Sandbox(SandboxErr::Denied {
        output: Box::new(output),
        network_policy_decision: None,
    });
    assert_eq!(
        get_error_message_ui(&err),
        "команда в песочнице завершилась с кодом 13"
    );
}

#[test]
fn usage_limit_reached_error_formats_free_plan() {
    let err = UsageLimitReachedError {
        plan_type: Some(PlanType::Known(KnownPlan::Free)),
        resets_at: None,
        rate_limits: Some(Box::new(rate_limit_snapshot())),
        promo_message: None,
    };
    assert_eq!(
        err.to_string(),
        "Вы достигли лимита использования. Обновите план до Plus, чтобы продолжить работу в Lavilas Codex (https://chatgpt.com/explore/plus), или повторите позже."
    );
}

#[test]
fn usage_limit_reached_error_formats_go_plan() {
    let err = UsageLimitReachedError {
        plan_type: Some(PlanType::Known(KnownPlan::Go)),
        resets_at: None,
        rate_limits: Some(Box::new(rate_limit_snapshot())),
        promo_message: None,
    };
    assert_eq!(
        err.to_string(),
        "Вы достигли лимита использования. Обновите план до Plus, чтобы продолжить работу в Lavilas Codex (https://chatgpt.com/explore/plus), или повторите позже."
    );
}

#[test]
fn usage_limit_reached_error_formats_default_when_none() {
    let err = UsageLimitReachedError {
        plan_type: None,
        resets_at: None,
        rate_limits: Some(Box::new(rate_limit_snapshot())),
        promo_message: None,
    };
    assert_eq!(
        err.to_string(),
        "Вы достигли лимита использования. Повторите позже."
    );
}

#[test]
fn usage_limit_reached_error_formats_team_plan() {
    let base = Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap();
    let resets_at = base + ChronoDuration::hours(1);
    with_now_override(base, move || {
        let expected_time = format_retry_timestamp(&resets_at);
        let err = UsageLimitReachedError {
            plan_type: Some(PlanType::Known(KnownPlan::Team)),
            resets_at: Some(resets_at),
            rate_limits: Some(Box::new(rate_limit_snapshot())),
            promo_message: None,
        };
        let expected = format!(
            "Вы достигли лимита использования. Чтобы получить больше доступа прямо сейчас, отправьте запрос администратору или повторите в {expected_time}."
        );
        assert_eq!(err.to_string(), expected);
    });
}

#[test]
fn usage_limit_reached_error_formats_business_plan_without_reset() {
    let err = UsageLimitReachedError {
        plan_type: Some(PlanType::Known(KnownPlan::Business)),
        resets_at: None,
        rate_limits: Some(Box::new(rate_limit_snapshot())),
        promo_message: None,
    };
    assert_eq!(
        err.to_string(),
        "Вы достигли лимита использования. Чтобы получить больше доступа прямо сейчас, отправьте запрос администратору или повторите позже."
    );
}

#[test]
fn usage_limit_reached_error_formats_self_serve_business_usage_based_plan() {
    let err = UsageLimitReachedError {
        plan_type: Some(PlanType::Known(KnownPlan::SelfServeBusinessUsageBased)),
        resets_at: None,
        rate_limits: Some(Box::new(rate_limit_snapshot())),
        promo_message: None,
    };
    assert_eq!(
        err.to_string(),
        "Вы достигли лимита использования. Чтобы получить больше доступа прямо сейчас, отправьте запрос администратору или повторите позже."
    );
}

#[test]
fn usage_limit_reached_error_formats_enterprise_cbp_usage_based_plan() {
    let err = UsageLimitReachedError {
        plan_type: Some(PlanType::Known(KnownPlan::EnterpriseCbpUsageBased)),
        resets_at: None,
        rate_limits: Some(Box::new(rate_limit_snapshot())),
        promo_message: None,
    };
    assert_eq!(
        err.to_string(),
        "Вы достигли лимита использования. Чтобы получить больше доступа прямо сейчас, отправьте запрос администратору или повторите позже."
    );
}

#[test]
fn usage_limit_reached_error_formats_default_for_other_plans() {
    let err = UsageLimitReachedError {
        plan_type: Some(PlanType::Known(KnownPlan::Enterprise)),
        resets_at: None,
        rate_limits: Some(Box::new(rate_limit_snapshot())),
        promo_message: None,
    };
    assert_eq!(
        err.to_string(),
        "Вы достигли лимита использования. Повторите позже."
    );
}

#[test]
fn usage_limit_reached_error_formats_pro_plan_with_reset() {
    let base = Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap();
    let resets_at = base + ChronoDuration::hours(1);
    with_now_override(base, move || {
        let expected_time = format_retry_timestamp(&resets_at);
        let err = UsageLimitReachedError {
            plan_type: Some(PlanType::Known(KnownPlan::Pro)),
            resets_at: Some(resets_at),
            rate_limits: Some(Box::new(rate_limit_snapshot())),
            promo_message: None,
        };
        let expected = format!(
            "Вы достигли лимита использования. Перейдите на https://chatgpt.com/codex/settings/usage, чтобы купить дополнительные кредиты или повторите в {expected_time}."
        );
        assert_eq!(err.to_string(), expected);
    });
}

#[test]
fn usage_limit_reached_error_hides_upsell_for_non_codex_limit_name() {
    let base = Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap();
    let resets_at = base + ChronoDuration::hours(1);
    with_now_override(base, move || {
        let expected_time = format_retry_timestamp(&resets_at);
        let err = UsageLimitReachedError {
            plan_type: Some(PlanType::Known(KnownPlan::Plus)),
            resets_at: Some(resets_at),
            rate_limits: Some(Box::new(RateLimitSnapshot {
                limit_id: Some("codex_other".to_string()),
                limit_name: Some("codex_other".to_string()),
                ..rate_limit_snapshot()
            })),
            promo_message: Some(
                "Перейдите на https://chatgpt.com/codex/settings/usage, чтобы купить дополнительные кредиты"
                    .to_string(),
            ),
        };
        let expected = format!(
            "Вы достигли лимита использования для codex_other. Переключитесь на другую модель или повторите в {expected_time}."
        );
        assert_eq!(err.to_string(), expected);
    });
}

#[test]
fn usage_limit_reached_includes_minutes_when_available() {
    let base = Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap();
    let resets_at = base + ChronoDuration::minutes(5);
    with_now_override(base, move || {
        let expected_time = format_retry_timestamp(&resets_at);
        let err = UsageLimitReachedError {
            plan_type: None,
            resets_at: Some(resets_at),
            rate_limits: Some(Box::new(rate_limit_snapshot())),
            promo_message: None,
        };
        let expected = format!("Вы достигли лимита использования. Повторите в {expected_time}.");
        assert_eq!(err.to_string(), expected);
    });
}

#[test]
fn unexpected_status_cloudflare_html_is_simplified() {
    let err = UnexpectedResponseError {
        status: StatusCode::FORBIDDEN,
        body: "<html><body>Cloudflare error: Sorry, you have been blocked</body></html>"
            .to_string(),
        url: Some("http://example.com/blocked".to_string()),
        cf_ray: Some("ray-id".to_string()),
        request_id: None,
        identity_authorization_error: None,
        identity_error_code: None,
    };
    let status = StatusCode::FORBIDDEN.to_string();
    let url = "http://example.com/blocked";
    assert_eq!(
        err.to_string(),
        format!("{CLOUDFLARE_BLOCKED_MESSAGE} (status {status}), url: {url}, cf-ray: ray-id")
    );
}

#[test]
fn unexpected_status_non_html_is_unchanged() {
    let err = UnexpectedResponseError {
        status: StatusCode::FORBIDDEN,
        body: "plain text error".to_string(),
        url: Some("http://example.com/plain".to_string()),
        cf_ray: None,
        request_id: None,
        identity_authorization_error: None,
        identity_error_code: None,
    };
    let status = StatusCode::FORBIDDEN.to_string();
    let url = "http://example.com/plain";
    assert_eq!(
        err.to_string(),
        format!("неожиданный статус {status}: plain text error, url: {url}")
    );
}

#[test]
fn unexpected_status_prefers_error_message_when_present() {
    let err = UnexpectedResponseError {
        status: StatusCode::UNAUTHORIZED,
        body: r#"{"error":{"message":"Workspace is not authorized in this region."},"status":401}"#
            .to_string(),
        url: Some("https://chatgpt.com/backend-api/codex/responses".to_string()),
        cf_ray: None,
        request_id: Some("req-123".to_string()),
        identity_authorization_error: None,
        identity_error_code: None,
    };
    let status = StatusCode::UNAUTHORIZED.to_string();
    assert_eq!(
        err.to_string(),
        format!(
            "неожиданный статус {status}: Workspace is not authorized in this region., url: https://chatgpt.com/backend-api/codex/responses, id запроса: req-123"
        )
    );
}

#[test]
fn unexpected_status_truncates_long_body_with_ellipsis() {
    let long_body = "x".repeat(UNEXPECTED_RESPONSE_BODY_MAX_BYTES + 10);
    let err = UnexpectedResponseError {
        status: StatusCode::BAD_GATEWAY,
        body: long_body,
        url: Some("http://example.com/long".to_string()),
        cf_ray: None,
        request_id: Some("req-long".to_string()),
        identity_authorization_error: None,
        identity_error_code: None,
    };
    let status = StatusCode::BAD_GATEWAY.to_string();
    let expected_body = format!("{}...", "x".repeat(UNEXPECTED_RESPONSE_BODY_MAX_BYTES));
    assert_eq!(
        err.to_string(),
        format!(
            "неожиданный статус {status}: {expected_body}, url: http://example.com/long, id запроса: req-long"
        )
    );
}

#[test]
fn unexpected_status_includes_cf_ray_and_request_id() {
    let err = UnexpectedResponseError {
        status: StatusCode::UNAUTHORIZED,
        body: "plain text error".to_string(),
        url: Some("https://chatgpt.com/backend-api/codex/responses".to_string()),
        cf_ray: Some("9c81f9f18f2fa49d-LHR".to_string()),
        request_id: Some("req-xyz".to_string()),
        identity_authorization_error: None,
        identity_error_code: None,
    };
    let status = StatusCode::UNAUTHORIZED.to_string();
    assert_eq!(
        err.to_string(),
        format!(
            "неожиданный статус {status}: plain text error, url: https://chatgpt.com/backend-api/codex/responses, cf-ray: 9c81f9f18f2fa49d-LHR, id запроса: req-xyz"
        )
    );
}

#[test]
fn unexpected_status_includes_identity_auth_details() {
    let err = UnexpectedResponseError {
        status: StatusCode::UNAUTHORIZED,
        body: "plain text error".to_string(),
        url: Some("https://chatgpt.com/backend-api/codex/models".to_string()),
        cf_ray: Some("cf-ray-auth-401-test".to_string()),
        request_id: Some("req-auth".to_string()),
        identity_authorization_error: Some("missing_authorization_header".to_string()),
        identity_error_code: Some("token_expired".to_string()),
    };
    let status = StatusCode::UNAUTHORIZED.to_string();
    assert_eq!(
        err.to_string(),
        format!(
            "неожиданный статус {status}: plain text error, url: https://chatgpt.com/backend-api/codex/models, cf-ray: cf-ray-auth-401-test, id запроса: req-auth, ошибка авторизации: missing_authorization_header, код ошибки авторизации: token_expired"
        )
    );
}

#[test]
fn usage_limit_reached_includes_hours_and_minutes() {
    let base = Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap();
    let resets_at = base + ChronoDuration::hours(3) + ChronoDuration::minutes(32);
    with_now_override(base, move || {
        let expected_time = format_retry_timestamp(&resets_at);
        let err = UsageLimitReachedError {
            plan_type: Some(PlanType::Known(KnownPlan::Plus)),
            resets_at: Some(resets_at),
            rate_limits: Some(Box::new(rate_limit_snapshot())),
            promo_message: None,
        };
        let expected = format!(
            "Вы достигли лимита использования. Обновите план до Pro (https://chatgpt.com/explore/pro), перейдите на https://chatgpt.com/codex/settings/usage, чтобы купить дополнительные кредиты или повторите в {expected_time}."
        );
        assert_eq!(err.to_string(), expected);
    });
}

#[test]
fn usage_limit_reached_includes_days_hours_minutes() {
    let base = Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap();
    let resets_at =
        base + ChronoDuration::days(2) + ChronoDuration::hours(3) + ChronoDuration::minutes(5);
    with_now_override(base, move || {
        let expected_time = format_retry_timestamp(&resets_at);
        let err = UsageLimitReachedError {
            plan_type: None,
            resets_at: Some(resets_at),
            rate_limits: Some(Box::new(rate_limit_snapshot())),
            promo_message: None,
        };
        let expected = format!("Вы достигли лимита использования. Повторите в {expected_time}.");
        assert_eq!(err.to_string(), expected);
    });
}

#[test]
fn usage_limit_reached_less_than_minute() {
    let base = Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap();
    let resets_at = base + ChronoDuration::seconds(30);
    with_now_override(base, move || {
        let expected_time = format_retry_timestamp(&resets_at);
        let err = UsageLimitReachedError {
            plan_type: None,
            resets_at: Some(resets_at),
            rate_limits: Some(Box::new(rate_limit_snapshot())),
            promo_message: None,
        };
        let expected = format!("Вы достигли лимита использования. Повторите в {expected_time}.");
        assert_eq!(err.to_string(), expected);
    });
}

#[test]
fn usage_limit_reached_with_promo_message() {
    let base = Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap();
    let resets_at = base + ChronoDuration::seconds(30);
    with_now_override(base, move || {
        let expected_time = format_retry_timestamp(&resets_at);
        let err = UsageLimitReachedError {
            plan_type: None,
            resets_at: Some(resets_at),
            rate_limits: Some(Box::new(rate_limit_snapshot())),
            promo_message: Some(
                "Чтобы продолжить работу в Lavilas Codex, начните бесплатный пробный период <PLAN> уже сегодня".to_string(),
            ),
        };
        let expected = format!(
            "Вы достигли лимита использования. Чтобы продолжить работу в Lavilas Codex, начните бесплатный пробный период <PLAN> уже сегодня, или повторите в {expected_time}."
        );
        assert_eq!(err.to_string(), expected);
    });
}
