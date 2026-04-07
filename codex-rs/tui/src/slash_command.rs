use strum::IntoEnumIterator;
use strum_macros::AsRefStr;
use strum_macros::EnumIter;
use strum_macros::EnumString;
use strum_macros::IntoStaticStr;

/// Commands that can be invoked by starting a message with a leading slash.
#[derive(
    Debug, Clone, Copy, PartialEq, Eq, Hash, EnumString, EnumIter, AsRefStr, IntoStaticStr,
)]
#[strum(serialize_all = "kebab-case")]
pub enum SlashCommand {
    // DO NOT ALPHA-SORT! Enum order is presentation order in the popup, so
    // more frequently used commands should be listed first.
    #[strum(serialize = "model", serialize = "модель")]
    Model,
    #[strum(serialize = "profiles", serialize = "профили")]
    Profiles,
    #[strum(serialize = "setlang", serialize = "set-lang", serialize = "язык")]
    Setlang,
    #[strum(serialize = "fast", serialize = "быстро")]
    Fast,
    #[strum(serialize = "approvals", serialize = "подтверждения")]
    Approvals,
    #[strum(serialize = "permissions", serialize = "разрешения")]
    Permissions,
    #[strum(serialize = "setup-default-sandbox")]
    ElevateSandbox,
    #[strum(serialize = "sandbox-add-read-dir")]
    SandboxReadRoot,
    #[strum(serialize = "experimental", serialize = "эксперимент")]
    Experimental,
    #[strum(serialize = "skills", serialize = "навыки")]
    Skills,
    #[strum(serialize = "review", serialize = "ревью")]
    Review,
    #[strum(serialize = "rename", serialize = "переименовать")]
    Rename,
    #[strum(serialize = "new", serialize = "новый")]
    New,
    #[strum(serialize = "resume", serialize = "продолжить")]
    Resume,
    #[strum(serialize = "fork", serialize = "форк")]
    Fork,
    #[strum(serialize = "init", serialize = "инициализация")]
    Init,
    #[strum(serialize = "compact", serialize = "сжать")]
    Compact,
    #[strum(serialize = "plan", serialize = "план")]
    Plan,
    #[strum(serialize = "collab", serialize = "режим")]
    Collab,
    #[strum(serialize = "agent", serialize = "агент")]
    Agent,
    // Undo,
    #[strum(serialize = "diff", serialize = "дифф")]
    Diff,
    #[strum(serialize = "copy", serialize = "копировать")]
    Copy,
    #[strum(serialize = "file", serialize = "mention", serialize = "файл")]
    Mention,
    #[strum(serialize = "status", serialize = "статус")]
    Status,
    #[strum(serialize = "debug-config", serialize = "дебаг-конфиг")]
    DebugConfig,
    #[strum(serialize = "title", serialize = "заголовок")]
    Title,
    #[strum(serialize = "statusline", serialize = "статус-строка")]
    Statusline,
    #[strum(serialize = "theme", serialize = "тема")]
    Theme,
    #[strum(serialize = "mcp", serialize = "мсп")]
    Mcp,
    #[strum(serialize = "apps", serialize = "приложения")]
    Apps,
    #[strum(serialize = "plugins", serialize = "плагины")]
    Plugins,
    #[strum(serialize = "logout", serialize = "выйти-аккаунт")]
    Logout,
    #[strum(serialize = "quit", serialize = "выход")]
    Quit,
    #[strum(serialize = "exit", serialize = "закрыть")]
    Exit,
    #[strum(serialize = "feedback", serialize = "фидбек")]
    Feedback,
    #[strum(serialize = "rollout", serialize = "роллаут")]
    Rollout,
    #[strum(serialize = "ps", serialize = "процессы")]
    Ps,
    #[strum(to_string = "stop", serialize = "clean", serialize = "стоп")]
    Stop,
    #[strum(serialize = "clear", serialize = "очистить")]
    Clear,
    #[strum(serialize = "personality", serialize = "стиль")]
    Personality,
    #[strum(serialize = "realtime", serialize = "голос")]
    Realtime,
    #[strum(serialize = "settings", serialize = "настройки")]
    Settings,
    #[strum(serialize = "test-approval", serialize = "тест-подтверждения")]
    TestApproval,
    #[strum(serialize = "subagents")]
    MultiAgents,
    // Debugging commands.
    #[strum(serialize = "debug-m-drop")]
    MemoryDrop,
    #[strum(serialize = "debug-m-update")]
    MemoryUpdate,
}

impl SlashCommand {
    /// User-visible description shown in the popup.
    pub fn description(self) -> &'static str {
        match self {
            SlashCommand::Feedback => "отправить логи разработчикам",
            SlashCommand::New => "начать новый чат в текущей сессии",
            SlashCommand::Init => "создать AGENTS.md с инструкциями для Lavilas Codex",
            SlashCommand::Compact => "сжать диалог, чтобы не упереться в лимит контекста",
            SlashCommand::Review => "проверить текущие изменения и найти проблемы",
            SlashCommand::Rename => "переименовать текущий тред",
            SlashCommand::Resume => "продолжить сохранённый чат",
            SlashCommand::Clear => "очистить терминал и начать новый чат",
            SlashCommand::Fork => "сделать форк текущего чата",
            // SlashCommand::Undo => "ask Codex to undo a turn",
            SlashCommand::Quit | SlashCommand::Exit => "выйти из Lavilas Codex",
            SlashCommand::Diff => "показать git diff (включая untracked)",
            SlashCommand::Copy => "скопировать последний ответ Lavilas Codex в буфер обмена",
            SlashCommand::Mention => "упомянуть файл",
            SlashCommand::Skills => "использовать навыки для улучшения выполнения задач",
            SlashCommand::Status => "показать конфигурацию сессии и расход токенов",
            SlashCommand::DebugConfig => "показать слои конфига и источники требований для отладки",
            SlashCommand::Title => "настроить элементы в заголовке терминала",
            SlashCommand::Statusline => "настроить элементы в строке статуса",
            SlashCommand::Theme => "выбрать тему подсветки синтаксиса",
            SlashCommand::Ps => "показать фоновые терминалы",
            SlashCommand::Stop => "остановить все фоновые терминалы",
            SlashCommand::MemoryDrop => "НЕ ИСПОЛЬЗОВАТЬ",
            SlashCommand::MemoryUpdate => "НЕ ИСПОЛЬЗОВАТЬ",
            SlashCommand::Model => "выбрать модель и бюджет размышлений",
            SlashCommand::Profiles => {
                "открыть менеджер профилей аккаунтов (добавление аккаунта + список профилей)"
            }
            SlashCommand::Setlang => "выбрать язык интерфейса (резервный ввод: /setlang <ru|en>)",
            SlashCommand::Fast => {
                "переключить быстрый режим для максимальной скорости (расход плана x2)"
            }
            SlashCommand::Personality => "выбрать стиль общения Lavilas Codex",
            SlashCommand::Realtime => "переключить голосовой realtime-режим (эксперимент)",
            SlashCommand::Settings => "открыть единые настройки (профили, язык, префикс, команды)",
            SlashCommand::Plan => "переключиться в режим планирования",
            SlashCommand::Collab => "изменить режим совместной работы (эксперимент)",
            SlashCommand::Agent | SlashCommand::MultiAgents => "переключить активный тред агента",
            SlashCommand::Approvals => "выбрать, что разрешено Lavilas Codex",
            SlashCommand::Permissions => "выбрать, что разрешено Lavilas Codex",
            SlashCommand::ElevateSandbox => "настроить повышенную песочницу агента",
            SlashCommand::SandboxReadRoot => {
                "разрешить песочнице чтение каталога: /sandbox-add-read-dir <absolute_path>"
            }
            SlashCommand::Experimental => "переключить экспериментальные функции",
            SlashCommand::Mcp => "показать настроенные MCP-инструменты",
            SlashCommand::Apps => "управление приложениями",
            SlashCommand::Plugins => "просмотр плагинов",
            SlashCommand::Logout => "выйти из аккаунта Lavilas Codex",
            SlashCommand::Rollout => "показать путь к rollout-файлу",
            SlashCommand::TestApproval => "тест запроса подтверждения",
        }
    }

    /// Stable canonical command key (English) without the leading '/'.
    pub fn command_en(self) -> &'static str {
        match self {
            SlashCommand::Model => "model",
            SlashCommand::Profiles => "profiles",
            SlashCommand::Setlang => "setlang",
            SlashCommand::Fast => "fast",
            SlashCommand::Approvals => "approvals",
            SlashCommand::Permissions => "permissions",
            SlashCommand::ElevateSandbox => "setup-default-sandbox",
            SlashCommand::SandboxReadRoot => "sandbox-add-read-dir",
            SlashCommand::Experimental => "experimental",
            SlashCommand::Skills => "skills",
            SlashCommand::Review => "review",
            SlashCommand::Rename => "rename",
            SlashCommand::New => "new",
            SlashCommand::Resume => "resume",
            SlashCommand::Fork => "fork",
            SlashCommand::Init => "init",
            SlashCommand::Compact => "compact",
            SlashCommand::Plan => "plan",
            SlashCommand::Collab => "collab",
            SlashCommand::Agent => "agent",
            SlashCommand::Diff => "diff",
            SlashCommand::Copy => "copy",
            SlashCommand::Mention => "mention",
            SlashCommand::Status => "status",
            SlashCommand::DebugConfig => "debug-config",
            SlashCommand::Title => "title",
            SlashCommand::Statusline => "statusline",
            SlashCommand::Theme => "theme",
            SlashCommand::Mcp => "mcp",
            SlashCommand::Apps => "apps",
            SlashCommand::Plugins => "plugins",
            SlashCommand::Logout => "logout",
            SlashCommand::Quit => "quit",
            SlashCommand::Exit => "exit",
            SlashCommand::Feedback => "feedback",
            SlashCommand::Rollout => "rollout",
            SlashCommand::Ps => "ps",
            SlashCommand::Stop => "stop",
            SlashCommand::Clear => "clear",
            SlashCommand::Personality => "personality",
            SlashCommand::Realtime => "realtime",
            SlashCommand::Settings => "settings",
            SlashCommand::TestApproval => "test-approval",
            SlashCommand::MultiAgents => "subagents",
            SlashCommand::MemoryDrop => "debug-m-drop",
            SlashCommand::MemoryUpdate => "debug-m-update",
        }
    }

    /// Primary command string shown in slash menu.
    pub fn command(self) -> &'static str {
        self.command_en()
    }

    /// Additional aliases that should be discoverable in the command popup search.
    ///
    /// Canonical English keys from [`Self::command_en`] stay primary for display
    /// and persistence; these aliases are only for matching/discovery.
    pub fn popup_aliases(self) -> &'static [&'static str] {
        match self {
            SlashCommand::Model => &["модель"],
            SlashCommand::Profiles => &["профили"],
            SlashCommand::Setlang => &["set-lang", "язык"],
            SlashCommand::Fast => &["быстро"],
            SlashCommand::Approvals => &["подтверждения"],
            SlashCommand::Permissions => &["разрешения"],
            SlashCommand::ElevateSandbox => &[],
            SlashCommand::SandboxReadRoot => &[],
            SlashCommand::Experimental => &["эксперимент"],
            SlashCommand::Skills => &["навыки"],
            SlashCommand::Review => &["ревью"],
            SlashCommand::Rename => &["переименовать"],
            SlashCommand::New => &["новый"],
            SlashCommand::Resume => &["продолжить"],
            SlashCommand::Fork => &["форк"],
            SlashCommand::Init => &["инициализация"],
            SlashCommand::Compact => &["сжать"],
            SlashCommand::Plan => &["план"],
            SlashCommand::Collab => &["режим"],
            SlashCommand::Agent => &["агент"],
            SlashCommand::Diff => &["дифф"],
            SlashCommand::Copy => &["копировать"],
            SlashCommand::Mention => &["file", "файл"],
            SlashCommand::Status => &["статус"],
            SlashCommand::DebugConfig => &["дебаг-конфиг"],
            SlashCommand::Title => &["заголовок"],
            SlashCommand::Statusline => &["статус-строка"],
            SlashCommand::Theme => &["тема"],
            SlashCommand::Mcp => &["мсп"],
            SlashCommand::Apps => &["приложения"],
            SlashCommand::Plugins => &["плагины"],
            SlashCommand::Logout => &["выйти-аккаунт"],
            SlashCommand::Quit => &["выход"],
            SlashCommand::Exit => &["закрыть"],
            SlashCommand::Feedback => &["фидбек"],
            SlashCommand::Rollout => &["роллаут"],
            SlashCommand::Ps => &["процессы"],
            SlashCommand::Stop => &["clean", "стоп"],
            SlashCommand::Clear => &["очистить"],
            SlashCommand::Personality => &["стиль"],
            SlashCommand::Realtime => &["голос", "audio"],
            SlashCommand::Settings => &["настройки", "параметры"],
            SlashCommand::TestApproval => &["тест-подтверждения"],
            SlashCommand::MultiAgents => &[],
            SlashCommand::MemoryDrop => &[],
            SlashCommand::MemoryUpdate => &[],
        }
    }

    /// Whether this command supports inline args (for example `/review ...`).
    pub fn supports_inline_args(self) -> bool {
        matches!(
            self,
            SlashCommand::Review
                | SlashCommand::Rename
                | SlashCommand::Plan
                | SlashCommand::Fast
                | SlashCommand::Setlang
                | SlashCommand::Profiles
                | SlashCommand::SandboxReadRoot
        )
    }

    /// Whether this command can be run while a task is in progress.
    pub fn available_during_task(self) -> bool {
        match self {
            SlashCommand::New
            | SlashCommand::Resume
            | SlashCommand::Fork
            | SlashCommand::Init
            | SlashCommand::Compact
            // | SlashCommand::Undo
            | SlashCommand::Model
            | SlashCommand::Fast
            | SlashCommand::Personality
            | SlashCommand::Approvals
            | SlashCommand::Permissions
            | SlashCommand::ElevateSandbox
            | SlashCommand::SandboxReadRoot
            | SlashCommand::Experimental
            | SlashCommand::Review
            | SlashCommand::Plan
            | SlashCommand::Clear
            | SlashCommand::Logout
            | SlashCommand::MemoryDrop
            | SlashCommand::MemoryUpdate => false,
            SlashCommand::Profiles | SlashCommand::Setlang => true,
            SlashCommand::Diff
            | SlashCommand::Copy
            | SlashCommand::Rename
            | SlashCommand::Mention
            | SlashCommand::Skills
            | SlashCommand::Status
            | SlashCommand::DebugConfig
            | SlashCommand::Ps
            | SlashCommand::Stop
            | SlashCommand::Mcp
            | SlashCommand::Apps
            | SlashCommand::Plugins
            | SlashCommand::Feedback
            | SlashCommand::Quit
            | SlashCommand::Exit => true,
            SlashCommand::Rollout => true,
            SlashCommand::TestApproval => true,
            SlashCommand::Realtime => true,
            SlashCommand::Settings => true,
            SlashCommand::Collab => true,
            SlashCommand::Agent | SlashCommand::MultiAgents => true,
            SlashCommand::Statusline => false,
            SlashCommand::Theme => false,
            SlashCommand::Title => false,
        }
    }

    fn is_visible(self) -> bool {
        match self {
            SlashCommand::SandboxReadRoot => cfg!(target_os = "windows"),
            SlashCommand::Copy => !cfg!(target_os = "android"),
            SlashCommand::Rollout | SlashCommand::TestApproval => cfg!(debug_assertions),
            _ => true,
        }
    }
}

/// Return all built-in commands in a Vec paired with their command string.
pub fn built_in_slash_commands() -> Vec<(&'static str, SlashCommand)> {
    SlashCommand::iter()
        .filter(|command| command.is_visible())
        .map(|c| (c.command(), c))
        .collect()
}

#[cfg(test)]
mod tests {
    use pretty_assertions::assert_eq;
    use std::str::FromStr;

    use super::SlashCommand;

    #[test]
    fn stop_command_is_canonical_name() {
        assert_eq!(SlashCommand::Stop.command(), "stop");
    }

    #[test]
    fn clean_alias_parses_to_stop_command() {
        assert_eq!(SlashCommand::from_str("clean"), Ok(SlashCommand::Stop));
    }

    #[test]
    fn russian_profile_alias_parses() {
        assert_eq!(
            SlashCommand::from_str("профили"),
            Ok(SlashCommand::Profiles)
        );
    }

    #[test]
    fn russian_setlang_alias_parses() {
        assert_eq!(SlashCommand::from_str("язык"), Ok(SlashCommand::Setlang));
    }
}
