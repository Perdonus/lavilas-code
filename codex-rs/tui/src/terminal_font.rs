use std::fmt;
use std::io;
use std::io::IsTerminal;
use std::io::Write;
use std::io::stdout;

use crossterm::Command;
use ratatui::crossterm::execute;

#[derive(Debug, Clone, Copy, Eq, PartialEq)]
pub(crate) enum SetTerminalFontResult {
    Applied,
    NotTerminal,
    NoVisibleContent,
}

pub(crate) fn set_terminal_font(family: &str) -> io::Result<SetTerminalFontResult> {
    let family = sanitize_terminal_font_family(family);
    if family.is_empty() {
        return Ok(SetTerminalFontResult::NoVisibleContent);
    }

    if !stdout().is_terminal() {
        return Ok(SetTerminalFontResult::NotTerminal);
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
