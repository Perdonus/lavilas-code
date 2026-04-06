use std::sync::atomic::AtomicU8;
use std::sync::atomic::Ordering;

const DEFAULT_COMMAND_PREFIX: u8 = b'/';
static COMMAND_PREFIX: AtomicU8 = AtomicU8::new(DEFAULT_COMMAND_PREFIX);

pub fn command_prefix() -> char {
    COMMAND_PREFIX.load(Ordering::Relaxed) as char
}

pub fn set_command_prefix(prefix: char) {
    if !prefix.is_ascii_whitespace() && prefix.is_ascii() {
        COMMAND_PREFIX.store(prefix as u8, Ordering::Relaxed);
    }
}

pub fn starts_with_command_prefix(line: &str) -> bool {
    line.starts_with(command_prefix())
}

/// Parse a first-line slash command of the form `/name <rest>`.
/// Returns `(name, rest_after_name, rest_offset)` if the line begins with the configured
/// command prefix and contains a non-empty name; otherwise returns `None`.
///
/// `rest_offset` is the byte index into the original line where `rest_after_name`
/// starts after trimming leading whitespace (so `line[rest_offset..] == rest_after_name`).
pub fn parse_slash_name(line: &str) -> Option<(&str, &str, usize)> {
    let prefix = command_prefix();
    let stripped = line.strip_prefix(prefix)?;
    let mut name_end_in_stripped = stripped.len();
    for (idx, ch) in stripped.char_indices() {
        if ch.is_whitespace() {
            name_end_in_stripped = idx;
            break;
        }
    }
    let name = &stripped[..name_end_in_stripped];
    if name.is_empty() {
        return None;
    }
    let rest_untrimmed = &stripped[name_end_in_stripped..];
    let rest = rest_untrimmed.trim_start();
    let rest_start_in_stripped = name_end_in_stripped + (rest_untrimmed.len() - rest.len());
    let rest_offset = rest_start_in_stripped + prefix.len_utf8();
    Some((name, rest, rest_offset))
}
