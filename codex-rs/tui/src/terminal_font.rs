use std::fmt;
use std::io;
use std::io::IsTerminal;
use std::io::Write;
use std::io::stdout;

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
            "Lavilas попробует переключить гарнитуру сам, но итог зависит от терминала."
        }
        (true, TerminalFontSupport::ManualOnly) => {
            "Этот терминал не обещает автосмену гарнитуры. Шрифт сохранится как предпочтение, а применить его лучше вручную в настройках терминала."
        }
        (false, TerminalFontSupport::BestEffort) => {
            "Lavilas will try to switch the terminal font, but the result still depends on the terminal."
        }
        (false, TerminalFontSupport::ManualOnly) => {
            "This terminal does not reliably support automatic font switching. The font will be saved as a preference; apply it manually in the terminal settings."
        }
    }
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
