use crate::ThreadId;
use crate::auth::KnownPlan;
use crate::auth::PlanType;
pub use crate::auth::RefreshTokenFailedError;
pub use crate::auth::RefreshTokenFailedReason;
use crate::exec_output::ExecToolCallOutput;
use crate::network_policy::NetworkPolicyDecisionPayload;
use crate::protocol::CodexErrorInfo;
use crate::protocol::ErrorEvent;
use crate::protocol::RateLimitSnapshot;
use crate::protocol::TruncationPolicy;
use chrono::DateTime;
use chrono::Datelike;
use chrono::Local;
use chrono::Utc;
use codex_async_utils::CancelErr;
use codex_utils_string::truncate_middle_chars;
use codex_utils_string::truncate_middle_with_token_budget;
use reqwest::StatusCode;
use serde_json;
use std::io;
use std::time::Duration;
use thiserror::Error;
use tokio::task::JoinError;

pub type Result<T> = std::result::Result<T, CodexErr>;

/// Limit UI error messages to a reasonable size while keeping useful context.
const ERROR_MESSAGE_UI_MAX_BYTES: usize = 2 * 1024;

#[derive(Error, Debug)]
pub enum SandboxErr {
    /// Error from sandbox execution
    #[error(
        "песочница запретила выполнение, код выхода: {}, stdout: {}, stderr: {}",
        .output.exit_code, .output.stdout.text, .output.stderr.text
    )]
    Denied {
        output: Box<ExecToolCallOutput>,
        network_policy_decision: Option<NetworkPolicyDecisionPayload>,
    },

    /// Error from linux seccomp filter setup
    #[cfg(target_os = "linux")]
    #[error("ошибка настройки seccomp")]
    SeccompInstall(#[from] seccompiler::Error),

    /// Error from linux seccomp backend
    #[cfg(target_os = "linux")]
    #[error("ошибка бэкенда seccomp")]
    SeccompBackend(#[from] seccompiler::BackendError),

    /// Command timed out
    #[error("время выполнения команды истекло")]
    Timeout { output: Box<ExecToolCallOutput> },

    /// Command was killed by a signal
    #[error("команда была завершена сигналом")]
    Signal(i32),

    /// Error from linux landlock
    #[error("Landlock не смог полностью применить все правила песочницы")]
    LandlockRestrict,
}

#[derive(Error, Debug)]
pub enum CodexErr {
    #[error(
        "ход прерван. Что-то пошло не так? Нажмите `/feedback`, чтобы отправить отчёт об ошибке."
    )]
    TurnAborted,

    /// Returned by ResponsesClient when the SSE stream disconnects or errors out **after** the HTTP
    /// handshake has succeeded but **before** it finished emitting `response.completed`.
    ///
    /// The Session loop treats this as a transient error and will automatically retry the turn.
    ///
    /// Optionally includes the requested delay before retrying the turn.
    #[error("поток отключился до завершения: {0}")]
    Stream(String, Option<Duration>),
    #[error(
        "Lavilas Codex исчерпал доступное окно контекста модели. Начните новую ветку или очистите раннюю историю перед повтором."
    )]
    ContextWindowExceeded,
    #[error("ветка с id {0} не найдена")]
    ThreadNotFound(ThreadId),
    #[error("достигнут лимит веток агента (макс. {max_threads})")]
    AgentLimitReached { max_threads: usize },
    #[error("событие настройки сессии не было первым событием в потоке")]
    SessionConfiguredNotFirstEvent,
    /// Returned by run_command_stream when the spawned child process timed out (10s).
    #[error("таймаут ожидания завершения дочернего процесса")]
    Timeout,
    /// Returned by run_command_stream when the child could not be spawned (its stdout/stderr pipes
    /// could not be captured). Analogous to the previous `CodexError::Spawn` variant.
    #[error("не удалось запустить процесс: stdout/stderr дочернего процесса не перехвачены")]
    Spawn,
    /// Returned by run_command_stream when the user pressed Ctrl-C (SIGINT). Session uses this to
    /// surface a polite FunctionCallOutput back to the model instead of crashing the CLI.
    #[error(
        "прервано (Ctrl-C). Что-то пошло не так? Нажмите `/feedback`, чтобы отправить отчёт об ошибке."
    )]
    Interrupted,
    /// Unexpected HTTP status code.
    #[error("{0}")]
    UnexpectedStatus(UnexpectedResponseError),
    /// Invalid request.
    #[error("{0}")]
    InvalidRequest(String),
    /// Invalid image.
    #[error("Некорректное изображение")]
    InvalidImageRequest(),
    #[error("{0}")]
    UsageLimitReached(UsageLimitReachedError),
    #[error("Выбранная модель перегружена. Попробуйте другую модель.")]
    ServerOverloaded,
    #[error("{0}")]
    ResponseStreamFailed(ResponseStreamFailed),
    #[error("{0}")]
    ConnectionFailed(ConnectionFailedError),
    #[error("Квота исчерпана. Проверьте тариф и биллинг.")]
    QuotaExceeded,
    #[error(
        "Чтобы использовать Lavilas Codex с вашим тарифом ChatGPT, обновитесь до Plus: https://chatgpt.com/explore/plus."
    )]
    UsageNotIncluded,
    #[error("Сейчас наблюдается повышенная нагрузка, из-за чего возможны временные ошибки.")]
    InternalServerError,
    /// Retry limit exceeded.
    #[error("{0}")]
    RetryLimit(RetryLimitReachedError),
    /// Agent loop died unexpectedly
    #[error("внутренняя ошибка: цикл агента завершился неожиданно")]
    InternalAgentDied,
    /// Sandbox error
    #[error("ошибка песочницы: {0}")]
    Sandbox(#[from] SandboxErr),
    #[error("требуется codex-linux-sandbox, но он не предоставлен")]
    LandlockSandboxExecutableNotProvided,
    #[error("неподдерживаемая операция: {0}")]
    UnsupportedOperation(String),
    #[error("{0}")]
    RefreshTokenFailed(RefreshTokenFailedError),
    #[error("Критическая ошибка: {0}")]
    Fatal(String),
    // -----------------------------------------------------------------
    // Automatic conversions for common external error types
    // -----------------------------------------------------------------
    #[error(transparent)]
    Io(#[from] io::Error),
    #[error(transparent)]
    Json(#[from] serde_json::Error),
    #[cfg(target_os = "linux")]
    #[error(transparent)]
    LandlockRuleset(#[from] landlock::RulesetError),
    #[cfg(target_os = "linux")]
    #[error(transparent)]
    LandlockPathFd(#[from] landlock::PathFdError),
    #[error(transparent)]
    TokioJoin(#[from] JoinError),
    #[error("{0}")]
    EnvVar(EnvVarError),
}

impl From<CancelErr> for CodexErr {
    fn from(_: CancelErr) -> Self {
        CodexErr::TurnAborted
    }
}

impl CodexErr {
    pub fn is_retryable(&self) -> bool {
        match self {
            CodexErr::TurnAborted
            | CodexErr::Interrupted
            | CodexErr::EnvVar(_)
            | CodexErr::Fatal(_)
            | CodexErr::UsageNotIncluded
            | CodexErr::QuotaExceeded
            | CodexErr::InvalidImageRequest()
            | CodexErr::InvalidRequest(_)
            | CodexErr::RefreshTokenFailed(_)
            | CodexErr::UnsupportedOperation(_)
            | CodexErr::Sandbox(_)
            | CodexErr::LandlockSandboxExecutableNotProvided
            | CodexErr::RetryLimit(_)
            | CodexErr::ContextWindowExceeded
            | CodexErr::ThreadNotFound(_)
            | CodexErr::AgentLimitReached { .. }
            | CodexErr::Spawn
            | CodexErr::SessionConfiguredNotFirstEvent
            | CodexErr::UsageLimitReached(_)
            | CodexErr::ServerOverloaded => false,
            CodexErr::Stream(..)
            | CodexErr::Timeout
            | CodexErr::UnexpectedStatus(_)
            | CodexErr::ResponseStreamFailed(_)
            | CodexErr::ConnectionFailed(_)
            | CodexErr::InternalServerError
            | CodexErr::InternalAgentDied
            | CodexErr::Io(_)
            | CodexErr::Json(_)
            | CodexErr::TokioJoin(_) => true,
            #[cfg(target_os = "linux")]
            CodexErr::LandlockRuleset(_) | CodexErr::LandlockPathFd(_) => false,
        }
    }

    /// Minimal shim so that existing `e.downcast_ref::<CodexErr>()` checks continue to compile
    /// after replacing `anyhow::Error` in the return signature. This mirrors the behavior of
    /// `anyhow::Error::downcast_ref` but works directly on our concrete enum.
    pub fn downcast_ref<T: std::any::Any>(&self) -> Option<&T> {
        (self as &dyn std::any::Any).downcast_ref::<T>()
    }

    /// Translate core error to client-facing protocol error.
    pub fn to_codex_protocol_error(&self) -> CodexErrorInfo {
        match self {
            CodexErr::ContextWindowExceeded => CodexErrorInfo::ContextWindowExceeded,
            CodexErr::UsageLimitReached(_)
            | CodexErr::QuotaExceeded
            | CodexErr::UsageNotIncluded => CodexErrorInfo::UsageLimitExceeded,
            CodexErr::ServerOverloaded => CodexErrorInfo::ServerOverloaded,
            CodexErr::RetryLimit(_) => CodexErrorInfo::ResponseTooManyFailedAttempts {
                http_status_code: self.http_status_code_value(),
            },
            CodexErr::ConnectionFailed(_) => CodexErrorInfo::HttpConnectionFailed {
                http_status_code: self.http_status_code_value(),
            },
            CodexErr::ResponseStreamFailed(_) => CodexErrorInfo::ResponseStreamConnectionFailed {
                http_status_code: self.http_status_code_value(),
            },
            CodexErr::RefreshTokenFailed(_) => CodexErrorInfo::Unauthorized,
            CodexErr::SessionConfiguredNotFirstEvent
            | CodexErr::InternalServerError
            | CodexErr::InternalAgentDied => CodexErrorInfo::InternalServerError,
            CodexErr::UnsupportedOperation(_)
            | CodexErr::ThreadNotFound(_)
            | CodexErr::AgentLimitReached { .. } => CodexErrorInfo::BadRequest,
            CodexErr::Sandbox(_) => CodexErrorInfo::SandboxError,
            _ => CodexErrorInfo::Other,
        }
    }

    pub fn to_error_event(&self, message_prefix: Option<String>) -> ErrorEvent {
        let error_message = self.to_string();
        let message: String = match message_prefix {
            Some(prefix) => format!("{prefix}: {error_message}"),
            None => error_message,
        };
        ErrorEvent {
            message,
            codex_error_info: Some(self.to_codex_protocol_error()),
        }
    }

    pub fn http_status_code_value(&self) -> Option<u16> {
        let http_status_code = match self {
            CodexErr::RetryLimit(err) => Some(err.status),
            CodexErr::UnexpectedStatus(err) => Some(err.status),
            CodexErr::ConnectionFailed(err) => err.source.status(),
            CodexErr::ResponseStreamFailed(err) => err.source.status(),
            _ => None,
        };
        http_status_code.as_ref().map(StatusCode::as_u16)
    }
}

#[derive(Debug)]
pub struct ConnectionFailedError {
    pub source: reqwest::Error,
}

impl std::fmt::Display for ConnectionFailedError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "Ошибка соединения: {}", self.source)
    }
}

#[derive(Debug)]
pub struct ResponseStreamFailed {
    pub source: reqwest::Error,
    pub request_id: Option<String>,
}

impl std::fmt::Display for ResponseStreamFailed {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "Ошибка при чтении ответа сервера: {}{}",
            self.source,
            self.request_id
                .as_ref()
                .map(|id| format!(", id запроса: {id}"))
                .unwrap_or_default()
        )
    }
}

#[derive(Debug)]
pub struct UnexpectedResponseError {
    pub status: StatusCode,
    pub body: String,
    pub url: Option<String>,
    pub cf_ray: Option<String>,
    pub request_id: Option<String>,
    pub identity_authorization_error: Option<String>,
    pub identity_error_code: Option<String>,
}

const CLOUDFLARE_BLOCKED_MESSAGE: &str = "Доступ заблокирован Cloudflare. Обычно это происходит при подключении из ограниченного региона";
const UNEXPECTED_RESPONSE_BODY_MAX_BYTES: usize = 1000;

impl UnexpectedResponseError {
    fn display_body(&self) -> String {
        if let Some(message) = self.extract_error_message() {
            return message;
        }

        let trimmed_body = self.body.trim();
        if trimmed_body.is_empty() {
            return "Неизвестная ошибка".to_string();
        }

        truncate_with_ellipsis(trimmed_body, UNEXPECTED_RESPONSE_BODY_MAX_BYTES)
    }

    fn extract_error_message(&self) -> Option<String> {
        let json = serde_json::from_str::<serde_json::Value>(&self.body).ok()?;
        let message = json
            .get("error")
            .and_then(|error| error.get("message"))
            .and_then(serde_json::Value::as_str)?;
        let message = message.trim();
        if message.is_empty() {
            None
        } else {
            Some(message.to_string())
        }
    }

    fn friendly_message(&self) -> Option<String> {
        if self.status != StatusCode::FORBIDDEN {
            return None;
        }

        if !self.body.contains("Cloudflare") || !self.body.contains("blocked") {
            return None;
        }

        let status = self.status;
        let mut message = format!("{CLOUDFLARE_BLOCKED_MESSAGE} (status {status})");
        if let Some(url) = &self.url {
            message.push_str(&format!(", url: {url}"));
        }
        if let Some(cf_ray) = &self.cf_ray {
            message.push_str(&format!(", cf-ray: {cf_ray}"));
        }
        if let Some(id) = &self.request_id {
            message.push_str(&format!(", id запроса: {id}"));
        }
        if let Some(auth_error) = &self.identity_authorization_error {
            message.push_str(&format!(", ошибка авторизации: {auth_error}"));
        }
        if let Some(error_code) = &self.identity_error_code {
            message.push_str(&format!(", код ошибки авторизации: {error_code}"));
        }

        Some(message)
    }
}

impl std::fmt::Display for UnexpectedResponseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if let Some(friendly) = self.friendly_message() {
            write!(f, "{friendly}")
        } else {
            let status = self.status;
            let body = self.display_body();
            let mut message = format!("неожиданный статус {status}: {body}");
            if let Some(url) = &self.url {
                message.push_str(&format!(", url: {url}"));
            }
            if let Some(cf_ray) = &self.cf_ray {
                message.push_str(&format!(", cf-ray: {cf_ray}"));
            }
            if let Some(id) = &self.request_id {
                message.push_str(&format!(", id запроса: {id}"));
            }
            if let Some(auth_error) = &self.identity_authorization_error {
                message.push_str(&format!(", ошибка авторизации: {auth_error}"));
            }
            if let Some(error_code) = &self.identity_error_code {
                message.push_str(&format!(", код ошибки авторизации: {error_code}"));
            }
            write!(f, "{message}")
        }
    }
}

impl std::error::Error for UnexpectedResponseError {}

fn truncate_with_ellipsis(text: &str, max_bytes: usize) -> String {
    if text.len() <= max_bytes {
        return text.to_string();
    }

    let mut cut = max_bytes;
    while !text.is_char_boundary(cut) {
        cut = cut.saturating_sub(1);
    }
    let mut truncated = text[..cut].to_string();
    truncated.push_str("...");
    truncated
}

fn truncate_text(content: &str, policy: TruncationPolicy) -> String {
    match policy {
        TruncationPolicy::Bytes(bytes) => truncate_middle_chars(content, bytes),
        TruncationPolicy::Tokens(tokens) => truncate_middle_with_token_budget(content, tokens).0,
    }
}

#[derive(Debug)]
pub struct RetryLimitReachedError {
    pub status: StatusCode,
    pub request_id: Option<String>,
}

impl std::fmt::Display for RetryLimitReachedError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "превышен лимит повторов, последний статус: {}{}",
            self.status,
            self.request_id
                .as_ref()
                .map(|id| format!(", id запроса: {id}"))
                .unwrap_or_default()
        )
    }
}

#[derive(Debug)]
pub struct UsageLimitReachedError {
    pub plan_type: Option<PlanType>,
    pub resets_at: Option<DateTime<Utc>>,
    pub rate_limits: Option<Box<RateLimitSnapshot>>,
    pub promo_message: Option<String>,
}

impl std::fmt::Display for UsageLimitReachedError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if let Some(limit_name) = self
            .rate_limits
            .as_ref()
            .and_then(|snapshot| snapshot.limit_name.as_deref())
            .map(str::trim)
            .filter(|name| !name.is_empty())
            && !limit_name.eq_ignore_ascii_case("codex")
        {
            return write!(
                f,
                "Вы достигли лимита использования для {limit_name}. Переключитесь на другую модель{}",
                retry_suffix_after_or(self.resets_at.as_ref())
            );
        }

        if let Some(promo_message) = &self.promo_message {
            return write!(
                f,
                "Вы достигли лимита использования. {promo_message},{}",
                retry_suffix_after_or(self.resets_at.as_ref())
            );
        }

        let message = match self.plan_type.as_ref() {
            Some(PlanType::Known(KnownPlan::Plus)) => format!(
                "Вы достигли лимита использования. Обновите план до Pro (https://chatgpt.com/explore/pro), перейдите на https://chatgpt.com/codex/settings/usage, чтобы купить дополнительные кредиты{}",
                retry_suffix_after_or(self.resets_at.as_ref())
            ),
            Some(PlanType::Known(
                KnownPlan::Team
                | KnownPlan::SelfServeBusinessUsageBased
                | KnownPlan::Business
                | KnownPlan::EnterpriseCbpUsageBased,
            )) => {
                format!(
                    "Вы достигли лимита использования. Чтобы получить больше доступа прямо сейчас, отправьте запрос администратору{}",
                    retry_suffix_after_or(self.resets_at.as_ref())
                )
            }
            Some(PlanType::Known(KnownPlan::Free)) | Some(PlanType::Known(KnownPlan::Go)) => {
                format!(
                    "Вы достигли лимита использования. Обновите план до Plus, чтобы продолжить работу в Lavilas Codex (https://chatgpt.com/explore/plus),{}",
                    retry_suffix_after_or(self.resets_at.as_ref())
                )
            }
            Some(PlanType::Known(KnownPlan::Pro)) => format!(
                "Вы достигли лимита использования. Перейдите на https://chatgpt.com/codex/settings/usage, чтобы купить дополнительные кредиты{}",
                retry_suffix_after_or(self.resets_at.as_ref())
            ),
            Some(PlanType::Known(KnownPlan::Enterprise))
            | Some(PlanType::Known(KnownPlan::Edu)) => format!(
                "Вы достигли лимита использования.{}",
                retry_suffix(self.resets_at.as_ref())
            ),
            Some(PlanType::Unknown(_)) | None => format!(
                "Вы достигли лимита использования.{}",
                retry_suffix(self.resets_at.as_ref())
            ),
        };

        write!(f, "{message}")
    }
}

fn retry_suffix(resets_at: Option<&DateTime<Utc>>) -> String {
    if let Some(resets_at) = resets_at {
        let formatted = format_retry_timestamp(resets_at);
        format!(" Повторите в {formatted}.")
    } else {
        " Повторите позже.".to_string()
    }
}

fn retry_suffix_after_or(resets_at: Option<&DateTime<Utc>>) -> String {
    if let Some(resets_at) = resets_at {
        let formatted = format_retry_timestamp(resets_at);
        format!(" или повторите в {formatted}.")
    } else {
        " или повторите позже.".to_string()
    }
}

fn format_retry_timestamp(resets_at: &DateTime<Utc>) -> String {
    let local_reset = resets_at.with_timezone(&Local);
    let local_now = now_for_retry().with_timezone(&Local);
    if local_reset.date_naive() == local_now.date_naive() {
        local_reset.format("%-I:%M %p").to_string()
    } else {
        let suffix = day_suffix(local_reset.day());
        local_reset
            .format(&format!("%b %-d{suffix}, %Y %-I:%M %p"))
            .to_string()
    }
}

fn day_suffix(day: u32) -> &'static str {
    match day {
        11..=13 => "th",
        _ => match day % 10 {
            1 => "st",
            2 => "nd", // codespell:ignore
            3 => "rd",
            _ => "th",
        },
    }
}

#[cfg(test)]
thread_local! {
    static NOW_OVERRIDE: std::cell::RefCell<Option<DateTime<Utc>>> =
        const { std::cell::RefCell::new(None) };
}

fn now_for_retry() -> DateTime<Utc> {
    #[cfg(test)]
    {
        if let Some(now) = NOW_OVERRIDE.with(|cell| *cell.borrow()) {
            return now;
        }
    }
    Utc::now()
}

#[derive(Debug)]
pub struct EnvVarError {
    /// Name of the environment variable that is missing.
    pub var: String,
    /// Optional instructions to help the user get a valid value for the
    /// variable and set it.
    pub instructions: Option<String>,
}

impl std::fmt::Display for EnvVarError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "Отсутствует переменная окружения: `{}`.", self.var)?;
        if let Some(instructions) = &self.instructions {
            write!(f, " {instructions}")?;
        }
        Ok(())
    }
}

pub fn get_error_message_ui(e: &CodexErr) -> String {
    let message = match e {
        CodexErr::Sandbox(SandboxErr::Denied { output, .. }) => {
            let aggregated = output.aggregated_output.text.trim();
            if !aggregated.is_empty() {
                output.aggregated_output.text.clone()
            } else {
                let stderr = output.stderr.text.trim();
                let stdout = output.stdout.text.trim();
                match (stderr.is_empty(), stdout.is_empty()) {
                    (false, false) => format!("{stderr}\n{stdout}"),
                    (false, true) => output.stderr.text.clone(),
                    (true, false) => output.stdout.text.clone(),
                    (true, true) => format!(
                        "команда в песочнице завершилась с кодом {}",
                        output.exit_code
                    ),
                }
            }
        }
        // Timeouts are not sandbox errors from a UX perspective; present them plainly.
        CodexErr::Sandbox(SandboxErr::Timeout { output }) => {
            format!(
                "ошибка: время выполнения команды истекло через {} мс",
                output.duration.as_millis()
            )
        }
        _ => e.to_string(),
    };

    truncate_text(
        &message,
        TruncationPolicy::Bytes(ERROR_MESSAGE_UI_MAX_BYTES),
    )
}

#[cfg(test)]
#[path = "error_tests.rs"]
mod tests;
