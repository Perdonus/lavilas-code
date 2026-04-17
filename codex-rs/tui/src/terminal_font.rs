use std::env;
use std::fmt;
use std::fs;
use std::io;
use std::io::IsTerminal;
use std::io::Write;
use std::io::stdout;
use std::path::Path;
use std::path::PathBuf;
#[cfg(target_os = "linux")]
use std::process::Command as ProcessCommand;

use codex_terminal_detection::Multiplexer;
use codex_terminal_detection::TerminalName;
use codex_terminal_detection::terminal_info;
use crossterm::Command;
use ratatui::crossterm::execute;

#[derive(Debug, Clone, Copy, Eq, PartialEq)]
pub(crate) enum SetTerminalFontResult {
    Applied,
    NotTerminal,
    NoVisibleContent,
    ManualOnly,
}

#[derive(Debug, Clone, Copy, Eq, PartialEq)]
pub(crate) enum TerminalFontSupport {
    BestEffort,
    ManualOnly,
}

pub(crate) fn terminal_font_support() -> TerminalFontSupport {
    let info = terminal_info();
    if info.is_zellij() || matches!(info.multiplexer, Some(Multiplexer::Tmux { .. })) {
        return TerminalFontSupport::ManualOnly;
    }

    match info.name {
        TerminalName::VsCode
        | TerminalName::WindowsTerminal
        | TerminalName::WarpTerminal
        | TerminalName::Dumb => TerminalFontSupport::ManualOnly,
        _ => TerminalFontSupport::BestEffort,
    }
}

pub(crate) fn terminal_font_support_summary(is_ru: bool) -> &'static str {
    match (is_ru, terminal_font_support()) {
        (true, TerminalFontSupport::BestEffort) => "Автоприменение: best-effort",
        (true, TerminalFontSupport::ManualOnly) => "Автоприменение: вручную",
        (false, TerminalFontSupport::BestEffort) => "Auto-apply: best effort",
        (false, TerminalFontSupport::ManualOnly) => "Auto-apply: manual only",
    }
}

pub(crate) fn terminal_font_support_note(is_ru: bool) -> &'static str {
    match (is_ru, terminal_font_support()) {
        (true, TerminalFontSupport::BestEffort) => {
            "Lavilas работает только с общей гарнитурой терминала: он попробует выложить шрифт в пользовательский каталог ОС и переключить терминал, но итог всё равно зависит от эмулятора."
        }
        (true, TerminalFontSupport::ManualOnly) => {
            "Этот терминал не обещает автосмену гарнитуры. Шрифт сохранится как глобальное предпочтение для терминала, а применить его лучше вручную в настройках терминала."
        }
        (false, TerminalFontSupport::BestEffort) => {
            "Lavilas only works with the terminal's global font. It can place the font into the OS user font directory and try to switch the terminal, but the result still depends on the terminal."
        }
        (false, TerminalFontSupport::ManualOnly) => {
            "This terminal does not reliably support automatic font switching. The font will be saved as a global terminal preference; apply it manually in the terminal settings."
        }
    }
}

pub(crate) fn terminal_font_install_dir() -> Option<PathBuf> {
    #[cfg(target_os = "linux")]
    {
        return user_home_dir().map(|home| home.join(".local/share/fonts/Lavilas Codex"));
    }

    #[cfg(target_os = "macos")]
    {
        return user_home_dir().map(|home| home.join("Library/Fonts/Lavilas Codex"));
    }

    #[cfg(target_os = "windows")]
    {
        let base = env::var_os("LOCALAPPDATA")
            .map(PathBuf::from)
            .or_else(|| user_home_dir().map(|home| home.join("AppData/Local")));
        return base.map(|dir| dir.join("Microsoft/Windows/Fonts/Lavilas Codex"));
    }

    #[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
    {
        None
    }
}

pub(crate) fn materialize_terminal_font_installation(
    font_id: &str,
    source_dir: &Path,
    source_files: &[String],
) -> io::Result<Option<PathBuf>> {
    let Some(root_dir) = terminal_font_install_dir() else {
        return Ok(None);
    };

    fs::create_dir_all(&root_dir)?;

    let stable_id = sanitize_terminal_font_path_component(font_id);
    if stable_id.is_empty() {
        return Ok(None);
    }

    let staging_dir = root_dir.join(format!(".{stable_id}.tmp-{}", std::process::id()));
    if staging_dir.exists() {
        let _ = fs::remove_dir_all(&staging_dir);
    }
    fs::create_dir_all(&staging_dir)?;

    let mut copied_any = false;
    for source_file in source_files {
        let Some(file_name) = Path::new(source_file).file_name().and_then(|name| name.to_str()) else {
            continue;
        };
        let source_path = source_dir.join(file_name);
        if !source_path.is_file() || !is_installable_font_file(&source_path) {
            continue;
        }
        fs::copy(&source_path, staging_dir.join(file_name))?;
        copied_any = true;
    }

    if !copied_any {
        let _ = fs::remove_dir_all(&staging_dir);
        return Ok(None);
    }

    let target_dir = root_dir.join(&stable_id);
    if target_dir.exists() {
        fs::remove_dir_all(&target_dir)?;
    }
    fs::rename(&staging_dir, &target_dir)?;
    refresh_terminal_font_cache(&root_dir);

    Ok(Some(target_dir))
}

pub(crate) fn remove_materialized_terminal_font(font_id: &str) -> io::Result<()> {
    let Some(root_dir) = terminal_font_install_dir() else {
        return Ok(());
    };

    let stable_id = sanitize_terminal_font_path_component(font_id);
    if stable_id.is_empty() {
        return Ok(());
    }

    let target_dir = root_dir.join(stable_id);
    if target_dir.exists() {
        fs::remove_dir_all(&target_dir)?;
        refresh_terminal_font_cache(&root_dir);
    }

    Ok(())
}

pub(crate) fn set_terminal_font(family: &str) -> io::Result<SetTerminalFontResult> {
    let family = sanitize_terminal_font_family(family);
    if family.is_empty() {
        return Ok(SetTerminalFontResult::NoVisibleContent);
    }

    if !stdout().is_terminal() {
        return Ok(SetTerminalFontResult::NotTerminal);
    }

    if matches!(terminal_font_support(), TerminalFontSupport::ManualOnly) {
        return Ok(SetTerminalFontResult::ManualOnly);
    }

    #[cfg(unix)]
    {
        let mut tty = std::fs::OpenOptions::new().write(true).open("/dev/tty")?;
        write!(tty, "\x1b]50;{}\x07", family)?;
        write!(tty, "\x1b]50;{}\x1b\\", family)?;
        tty.flush()?;
    }
    #[cfg(windows)]
    {
        execute!(stdout(), SetWindowFont(family))?;
    }

    Ok(SetTerminalFontResult::Applied)
}

#[cfg(windows)]
#[derive(Debug, Clone)]
struct SetWindowFont(String);

#[cfg(windows)]
impl Command for SetWindowFont {
    fn write_ansi(&self, f: &mut impl fmt::Write) -> fmt::Result {
        write!(f, "\x1b]50;{}\x07", self.0)
    }

    fn execute_winapi(&self) -> io::Result<()> {
        Err(std::io::Error::other(
            "tried to execute SetWindowFont using WinAPI; use ANSI instead",
        ))
    }

    fn is_ansi_code_supported(&self) -> bool {
        true
    }
}

fn sanitize_terminal_font_family(family: &str) -> String {
    let mut out = String::new();
    let mut pending_space = false;

    for ch in family.chars() {
        if ch.is_whitespace() {
            pending_space = !out.is_empty();
            continue;
        }
        if ch.is_control() || matches!(ch, '\u{202A}'..='\u{202E}' | '\u{2066}'..='\u{2069}') {
            continue;
        }
        if pending_space {
            out.push(' ');
            pending_space = false;
        }
        out.push(ch);
    }

    out
}

fn sanitize_terminal_font_path_component(raw: &str) -> String {
    let mut out = String::new();
    let mut pending_dash = false;

    for ch in raw.trim().chars() {
        let lower = ch.to_ascii_lowercase();
        if lower.is_ascii_alphanumeric() {
            out.push(lower);
            pending_dash = false;
            continue;
        }
        if matches!(lower, '-' | '_' | '.') && !out.is_empty() && !pending_dash {
            out.push('-');
            pending_dash = true;
        }
    }

    while out.ends_with('-') {
        out.pop();
    }

    out
}

fn is_installable_font_file(path: &Path) -> bool {
    matches!(
        path.extension()
            .and_then(|ext| ext.to_str())
            .map(|ext| ext.to_ascii_lowercase()),
        Some(ext) if matches!(ext.as_str(), "ttf" | "otf" | "woff" | "woff2")
    )
}

fn user_home_dir() -> Option<PathBuf> {
    #[cfg(windows)]
    {
        env::var_os("USERPROFILE")
            .map(PathBuf::from)
            .or_else(|| env::var_os("HOME").map(PathBuf::from))
    }

    #[cfg(not(windows))]
    {
        env::var_os("HOME").map(PathBuf::from)
    }
}

#[cfg(target_os = "linux")]
fn refresh_terminal_font_cache(font_dir: &Path) {
    let _ = ProcessCommand::new("fc-cache")
        .arg("-f")
        .arg(font_dir)
        .status();
}

#[cfg(not(target_os = "linux"))]
fn refresh_terminal_font_cache(_: &Path) {}
