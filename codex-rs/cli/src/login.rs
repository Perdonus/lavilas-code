//! CLI login commands and their direct-user observability surfaces.
//!
//! The TUI path already installs a broader tracing stack with feedback, OpenTelemetry, and other
//! interactive-session layers. Direct `codex login` intentionally does less: it preserves the
//! existing stderr/browser UX and adds only a small file-backed tracing layer for login-specific
//! targets. Keeping that setup local avoids pulling the TUI's session-oriented logging machinery
//! into a one-shot CLI command while still producing a durable `codex-login.log` artifact that
//! support can request from users.

use codex_app_server_protocol::AuthMode;
use codex_core::config::Config;
use codex_login::AuthCredentialsStoreMode;
use codex_login::CLIENT_ID;
use codex_login::CodexAuth;
use codex_login::ServerOptions;
use codex_login::login_with_api_key;
use codex_login::logout;
use codex_login::run_device_code_login;
use codex_login::run_login_server;
use codex_protocol::config_types::ForcedLoginMethod;
use codex_utils_cli::CliConfigOverrides;
use std::fs::OpenOptions;
use std::io::IsTerminal;
use std::io::Read;
use std::path::PathBuf;
use tracing_appender::non_blocking;
use tracing_appender::non_blocking::WorkerGuard;
use tracing_subscriber::EnvFilter;
use tracing_subscriber::Layer;
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::util::SubscriberInitExt;

const CHATGPT_LOGIN_DISABLED_MESSAGE: &str =
    "Вход через ChatGPT отключён. Используйте вход по API-ключу.";
const API_KEY_LOGIN_DISABLED_MESSAGE: &str =
    "Вход по API-ключу отключён. Используйте вход через ChatGPT.";
const LOGIN_SUCCESS_MESSAGE: &str = "Вход выполнен успешно";

/// Installs a small file-backed tracing layer for direct `codex login` flows.
///
/// This deliberately duplicates a narrow slice of the TUI logging setup instead of reusing it
/// wholesale. The TUI stack includes session-oriented layers that are valuable for interactive
/// runs but unnecessary for a one-shot login command. Keeping the direct CLI path local lets this
/// command produce a durable `codex-login.log` artifact without coupling it to the TUI's broader
/// telemetry and feedback initialization.
fn init_login_file_logging(config: &Config) -> Option<WorkerGuard> {
    let log_dir = match codex_core::config::log_dir(config) {
        Ok(log_dir) => log_dir,
        Err(err) => {
            eprintln!("Предупреждение: не удалось определить каталог логов входа: {err}");
            return None;
        }
    };

    if let Err(err) = std::fs::create_dir_all(&log_dir) {
        eprintln!(
            "Предупреждение: не удалось создать каталог логов входа {}: {err}",
            log_dir.display()
        );
        return None;
    }

    let mut log_file_opts = OpenOptions::new();
    log_file_opts.create(true).append(true);

    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        log_file_opts.mode(0o600);
    }

    let log_path = log_dir.join("codex-login.log");
    let log_file = match log_file_opts.open(&log_path) {
        Ok(log_file) => log_file,
        Err(err) => {
            eprintln!(
            "Предупреждение: не удалось открыть файл логов входа {}: {err}",
                log_path.display()
            );
            return None;
        }
    };

    let (non_blocking, guard) = non_blocking(log_file);
    let env_filter = EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| EnvFilter::new("codex_cli=info,codex_core=info,codex_login=info"));
    let file_layer = tracing_subscriber::fmt::layer()
        .with_writer(non_blocking)
        .with_target(true)
        .with_ansi(false)
        .with_filter(env_filter);

    // Direct `codex login` otherwise relies on ephemeral stderr and browser output.
    // Persist the same login targets to a file so support can inspect auth failures
    // without reproducing them through TUI or app-server.
    if let Err(err) = tracing_subscriber::registry().with(file_layer).try_init() {
        eprintln!(
            "Предупреждение: не удалось инициализировать файл логов входа {}: {err}",
            log_path.display()
        );
        return None;
    }

    Some(guard)
}

fn print_login_server_start(actual_port: u16, auth_url: &str) {
    eprintln!(
        "Запускаю локальный сервер входа на http://localhost:{actual_port}.\nЕсли браузер не открылся, перейдите по этой ссылке для входа:\n\n{auth_url}\n\nЕсли машина удалённая или без браузера, используйте `lavilas login --device-auth`."
    );
}

pub async fn login_with_chatgpt(
    codex_home: PathBuf,
    forced_chatgpt_workspace_id: Option<String>,
    cli_auth_credentials_store_mode: AuthCredentialsStoreMode,
) -> std::io::Result<()> {
    let opts = ServerOptions::new(
        codex_home,
        CLIENT_ID.to_string(),
        forced_chatgpt_workspace_id,
        cli_auth_credentials_store_mode,
    );
    let server = run_login_server(opts)?;

    print_login_server_start(server.actual_port, &server.auth_url);

    server.block_until_done().await
}

pub async fn run_login_with_chatgpt(cli_config_overrides: CliConfigOverrides) -> ! {
    let config = load_config_or_exit(cli_config_overrides).await;
    let _login_log_guard = init_login_file_logging(&config);
    tracing::info!("starting browser login flow");

    if matches!(config.forced_login_method, Some(ForcedLoginMethod::Api)) {
        eprintln!("{CHATGPT_LOGIN_DISABLED_MESSAGE}");
        std::process::exit(1);
    }

    let forced_chatgpt_workspace_id = config.forced_chatgpt_workspace_id.clone();

    match login_with_chatgpt(
        config.codex_home,
        forced_chatgpt_workspace_id,
        config.cli_auth_credentials_store_mode,
    )
    .await
    {
        Ok(_) => {
            eprintln!("{LOGIN_SUCCESS_MESSAGE}");
            std::process::exit(0);
        }
        Err(e) => {
            eprintln!("Ошибка входа: {e}");
            std::process::exit(1);
        }
    }
}

pub async fn run_login_with_api_key(
    cli_config_overrides: CliConfigOverrides,
    api_key: String,
) -> ! {
    let config = load_config_or_exit(cli_config_overrides).await;
    let _login_log_guard = init_login_file_logging(&config);
    tracing::info!("starting api key login flow");

    if matches!(config.forced_login_method, Some(ForcedLoginMethod::Chatgpt)) {
        eprintln!("{API_KEY_LOGIN_DISABLED_MESSAGE}");
        std::process::exit(1);
    }

    match login_with_api_key(
        &config.codex_home,
        &api_key,
        config.cli_auth_credentials_store_mode,
    ) {
        Ok(_) => {
            eprintln!("{LOGIN_SUCCESS_MESSAGE}");
            std::process::exit(0);
        }
        Err(e) => {
            eprintln!("Ошибка входа: {e}");
            std::process::exit(1);
        }
    }
}

pub fn read_api_key_from_stdin() -> String {
    let mut stdin = std::io::stdin();

    if stdin.is_terminal() {
        eprintln!(
            "--with-api-key ожидает API-ключ через stdin. Например: `printenv OPENAI_API_KEY | lavilas login --with-api-key`."
        );
        std::process::exit(1);
    }

    eprintln!("Читаю API-ключ из stdin...");

    let mut buffer = String::new();
    if let Err(err) = stdin.read_to_string(&mut buffer) {
        eprintln!("Не удалось прочитать API-ключ из stdin: {err}");
        std::process::exit(1);
    }

    let api_key = buffer.trim().to_string();
    if api_key.is_empty() {
        eprintln!("API-ключ через stdin не был передан.");
        std::process::exit(1);
    }

    api_key
}

/// Login using the OAuth код устройства flow.
pub async fn run_login_with_device_code(
    cli_config_overrides: CliConfigOverrides,
    issuer_base_url: Option<String>,
    client_id: Option<String>,
) -> ! {
    let config = load_config_or_exit(cli_config_overrides).await;
    let _login_log_guard = init_login_file_logging(&config);
    tracing::info!("starting код устройства login flow");
    if matches!(config.forced_login_method, Some(ForcedLoginMethod::Api)) {
        eprintln!("{CHATGPT_LOGIN_DISABLED_MESSAGE}");
        std::process::exit(1);
    }
    let forced_chatgpt_workspace_id = config.forced_chatgpt_workspace_id.clone();
    let mut opts = ServerOptions::new(
        config.codex_home,
        client_id.unwrap_or(CLIENT_ID.to_string()),
        forced_chatgpt_workspace_id,
        config.cli_auth_credentials_store_mode,
    );
    if let Some(iss) = issuer_base_url {
        opts.issuer = iss;
    }
    match run_device_code_login(opts).await {
        Ok(()) => {
            eprintln!("{LOGIN_SUCCESS_MESSAGE}");
            std::process::exit(0);
        }
        Err(e) => {
            eprintln!("Ошибка входа через код устройства: {e}");
            std::process::exit(1);
        }
    }
}

/// Prefers device-code login (with `open_browser = false`) when headless environment is detected, but keeps
/// `codex login` working in environments where device-code may be disabled/feature-gated.
/// If `run_device_code_login` returns `ErrorKind::NotFound` ("код устройства не поддерживается"), this
/// falls back to starting the local browser login server.
pub async fn run_login_with_device_code_fallback_to_browser(
    cli_config_overrides: CliConfigOverrides,
    issuer_base_url: Option<String>,
    client_id: Option<String>,
) -> ! {
    let config = load_config_or_exit(cli_config_overrides).await;
    let _login_log_guard = init_login_file_logging(&config);
    tracing::info!("starting login flow with код устройства fallback");
    if matches!(config.forced_login_method, Some(ForcedLoginMethod::Api)) {
        eprintln!("{CHATGPT_LOGIN_DISABLED_MESSAGE}");
        std::process::exit(1);
    }

    let forced_chatgpt_workspace_id = config.forced_chatgpt_workspace_id.clone();
    let mut opts = ServerOptions::new(
        config.codex_home,
        client_id.unwrap_or(CLIENT_ID.to_string()),
        forced_chatgpt_workspace_id,
        config.cli_auth_credentials_store_mode,
    );
    if let Some(iss) = issuer_base_url {
        opts.issuer = iss;
    }
    opts.open_browser = false;

    match run_device_code_login(opts.clone()).await {
        Ok(()) => {
            eprintln!("{LOGIN_SUCCESS_MESSAGE}");
            std::process::exit(0);
        }
        Err(e) => {
            if e.kind() == std::io::ErrorKind::NotFound {
                eprintln!("Вход через код устройства недоступен; переключаюсь на вход через браузер.");
                match run_login_server(opts) {
                    Ok(server) => {
                        print_login_server_start(server.actual_port, &server.auth_url);
                        match server.block_until_done().await {
                            Ok(()) => {
                                eprintln!("{LOGIN_SUCCESS_MESSAGE}");
                                std::process::exit(0);
                            }
                            Err(e) => {
                                eprintln!("Ошибка входа: {e}");
                                std::process::exit(1);
                            }
                        }
                    }
                    Err(e) => {
                        eprintln!("Ошибка входа: {e}");
                        std::process::exit(1);
                    }
                }
            } else {
                eprintln!("Ошибка входа через код устройства: {e}");
                std::process::exit(1);
            }
        }
    }
}

pub async fn run_login_status(cli_config_overrides: CliConfigOverrides) -> ! {
    let config = load_config_or_exit(cli_config_overrides).await;

    match CodexAuth::from_auth_storage(&config.codex_home, config.cli_auth_credentials_store_mode) {
        Ok(Some(auth)) => match auth.auth_mode() {
            AuthMode::ApiKey => match auth.get_token() {
                Ok(api_key) => {
                    eprintln!("Вход выполнен по API-ключу — {}", safe_format_key(&api_key));
                    std::process::exit(0);
                }
                Err(e) => {
                    eprintln!("Неожиданная ошибка при чтении API-ключа: {e}");
                    std::process::exit(1);
                }
            },
            AuthMode::Chatgpt | AuthMode::ChatgptAuthTokens => {
                eprintln!("Вход выполнен через ChatGPT");
                std::process::exit(0);
            }
        },
        Ok(None) => {
            eprintln!("Вход не выполнен");
            std::process::exit(1);
        }
        Err(e) => {
            eprintln!("Ошибка при проверке состояния входа: {e}");
            std::process::exit(1);
        }
    }
}

pub async fn run_logout(cli_config_overrides: CliConfigOverrides) -> ! {
    let config = load_config_or_exit(cli_config_overrides).await;

    match logout(&config.codex_home, config.cli_auth_credentials_store_mode) {
        Ok(true) => {
            eprintln!("Выход выполнен успешно");
            std::process::exit(0);
        }
        Ok(false) => {
            eprintln!("Вход не выполнен");
            std::process::exit(0);
        }
        Err(e) => {
            eprintln!("Ошибка при выходе: {e}");
            std::process::exit(1);
        }
    }
}

async fn load_config_or_exit(cli_config_overrides: CliConfigOverrides) -> Config {
    let cli_overrides = match cli_config_overrides.parse_overrides() {
        Ok(v) => v,
        Err(e) => {
            eprintln!("Ошибка разбора параметров -c: {e}");
            std::process::exit(1);
        }
    };

    match Config::load_with_cli_overrides(cli_overrides).await {
        Ok(config) => config,
        Err(e) => {
            eprintln!("Ошибка загрузки конфигурации: {e}");
            std::process::exit(1);
        }
    }
}

fn safe_format_key(key: &str) -> String {
    if key.len() <= 13 {
        return "***".to_string();
    }
    let prefix = &key[..8];
    let suffix = &key[key.len() - 5..];
    format!("{prefix}***{suffix}")
}

#[cfg(test)]
mod tests {
    use super::safe_format_key;

    #[test]
    fn formats_long_key() {
        let key = "sk-proj-1234567890ABCDE";
        assert_eq!(safe_format_key(key), "sk-proj-***ABCDE");
    }

    #[test]
    fn short_key_returns_stars() {
        let key = "sk-proj-12345";
        assert_eq!(safe_format_key(key), "***");
    }
}
