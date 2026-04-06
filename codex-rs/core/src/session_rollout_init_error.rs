use std::io::ErrorKind;
use std::path::Path;

use crate::rollout::SESSIONS_SUBDIR;
use codex_protocol::error::CodexErr;

pub(crate) fn map_session_init_error(err: &anyhow::Error, codex_home: &Path) -> CodexErr {
    if let Some(mapped) = err
        .chain()
        .filter_map(|cause| cause.downcast_ref::<std::io::Error>())
        .find_map(|io_err| map_rollout_io_error(io_err, codex_home))
    {
        return mapped;
    }

    CodexErr::Fatal(format!("Не удалось инициализировать сессию: {err:#}"))
}

fn map_rollout_io_error(io_err: &std::io::Error, codex_home: &Path) -> Option<CodexErr> {
    let sessions_dir = codex_home.join(SESSIONS_SUBDIR);
    let hint = match io_err.kind() {
        ErrorKind::PermissionDenied => format!(
            "Lavilas Codex не может получить доступ к файлам сессий в {} (доступ запрещён). Если сессии были созданы через sudo, исправьте владельца: sudo chown -R $(whoami) {}",
            sessions_dir.display(),
            codex_home.display()
        ),
        ErrorKind::NotFound => format!(
            "Хранилище сессий отсутствует по пути {}. Создайте директорию или укажите другой домашний каталог Lavilas Codex.",
            sessions_dir.display()
        ),
        ErrorKind::AlreadyExists => format!(
            "Путь хранилища сессий {} занят существующим файлом. Удалите или переименуйте его, чтобы Lavilas Codex смог создать сессии.",
            sessions_dir.display()
        ),
        ErrorKind::InvalidData | ErrorKind::InvalidInput => format!(
            "Данные сессий в {} выглядят повреждёнными или нечитаемыми. Может помочь очистка директории sessions (это удалит сохранённые диалоги).",
            sessions_dir.display()
        ),
        ErrorKind::IsADirectory | ErrorKind::NotADirectory => format!(
            "Путь хранилища сессий {} имеет неожиданный тип. Убедитесь, что это директория, которую Lavilas Codex может использовать для файлов сессий.",
            sessions_dir.display()
        ),
        _ => return None,
    };

    Some(CodexErr::Fatal(format!(
        "{hint} (исходная ошибка: {io_err})"
    )))
}
