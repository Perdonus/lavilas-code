use super::*;
use crate::tools::sandboxing::ToolError;
use codex_apply_patch::MaybeApplyPatchVerified;
use codex_protocol::error::CodexErr;
use codex_protocol::error::SandboxErr;
use codex_protocol::exec_output::ExecToolCallOutput;
use codex_protocol::exec_output::StreamOutput;
use codex_protocol::permissions::FileSystemSandboxPolicy;
use codex_protocol::protocol::SandboxPolicy;
use pretty_assertions::assert_eq;
use std::time::Duration;
use tempfile::TempDir;

#[test]
fn approval_keys_include_move_destination() {
    let tmp = TempDir::new().expect("tmp");
    let cwd = tmp.path();
    std::fs::create_dir_all(cwd.join("old")).expect("create old dir");
    std::fs::create_dir_all(cwd.join("renamed/dir")).expect("create dest dir");
    std::fs::write(cwd.join("old/name.txt"), "old content\n").expect("write old file");
    let patch = r#"*** Begin Patch
*** Update File: old/name.txt
*** Move to: renamed/dir/name.txt
@@
-old content
+new content
*** End Patch"#;
    let argv = vec!["apply_patch".to_string(), patch.to_string()];
    let action = match codex_apply_patch::maybe_parse_apply_patch_verified(&argv, cwd) {
        MaybeApplyPatchVerified::Body(action) => action,
        other => panic!("expected patch body, got: {other:?}"),
    };

    let keys = file_paths_for_action(&action);
    assert_eq!(keys.len(), 2);
}

#[test]
fn write_permissions_for_paths_skip_dirs_already_writable_under_workspace_root() {
    let tmp = TempDir::new().expect("tmp");
    let cwd = tmp.path();
    let nested = cwd.join("nested");
    std::fs::create_dir_all(&nested).expect("create nested dir");
    let file_path = AbsolutePathBuf::try_from(nested.join("file.txt"))
        .expect("nested file path should be absolute");
    let sandbox_policy = FileSystemSandboxPolicy::from(&SandboxPolicy::WorkspaceWrite {
        writable_roots: vec![],
        read_only_access: Default::default(),
        network_access: false,
        exclude_tmpdir_env_var: true,
        exclude_slash_tmp: false,
    });

    let permissions = write_permissions_for_paths(&[file_path], &sandbox_policy, cwd);

    assert_eq!(permissions, None);
}

#[test]
fn write_permissions_for_paths_keep_dirs_outside_workspace_root() {
    let tmp = TempDir::new().expect("tmp");
    let cwd = tmp.path().join("workspace");
    let outside = tmp.path().join("outside");
    std::fs::create_dir_all(&cwd).expect("create cwd");
    std::fs::create_dir_all(&outside).expect("create outside dir");
    let file_path = AbsolutePathBuf::try_from(outside.join("file.txt"))
        .expect("outside file path should be absolute");
    let sandbox_policy = FileSystemSandboxPolicy::from(&SandboxPolicy::WorkspaceWrite {
        writable_roots: vec![],
        read_only_access: Default::default(),
        network_access: false,
        exclude_tmpdir_env_var: true,
        exclude_slash_tmp: true,
    });

    let permissions = write_permissions_for_paths(&[file_path], &sandbox_policy, &cwd);
    let expected_outside = AbsolutePathBuf::from_absolute_path(dunce::simplified(
        &outside.canonicalize().expect("canonicalize outside dir"),
    ))
    .expect("outside dir should be absolute");

    assert_eq!(
        permissions.and_then(|profile| profile.file_system.and_then(|fs| fs.write)),
        Some(vec![expected_outside])
    );
}

#[test]
fn enrich_apply_patch_failure_output_uses_aggregated_output_when_stderr_missing() {
    let output = ExecToolCallOutput {
        exit_code: 1,
        stdout: StreamOutput::new(String::new()),
        stderr: StreamOutput::new(String::new()),
        aggregated_output: StreamOutput::new(
            "Failed to find expected lines in file.txt".to_string(),
        ),
        duration: Duration::from_millis(25),
        timed_out: false,
    };

    let enriched = enrich_apply_patch_failure_output(output);

    assert_eq!(
        enriched.stderr.text,
        "Failed to find expected lines in file.txt"
    );
}

#[test]
fn enrich_apply_patch_failure_output_falls_back_to_stdout_when_aggregated_output_missing() {
    let output = ExecToolCallOutput {
        exit_code: 2,
        stdout: StreamOutput::new("stdout fallback".to_string()),
        stderr: StreamOutput::new("\n".to_string()),
        aggregated_output: StreamOutput::new(String::new()),
        duration: Duration::from_millis(10),
        timed_out: false,
    };

    let enriched = enrich_apply_patch_failure_output(output);

    assert_eq!(enriched.stderr.text, "stdout fallback");
}

#[test]
fn enrich_apply_patch_failure_output_keeps_non_empty_stderr() {
    let output = ExecToolCallOutput {
        exit_code: 1,
        stdout: StreamOutput::new("stdout text".to_string()),
        stderr: StreamOutput::new("real stderr".to_string()),
        aggregated_output: StreamOutput::new("aggregated text".to_string()),
        duration: Duration::from_millis(12),
        timed_out: false,
    };

    let enriched = enrich_apply_patch_failure_output(output);

    assert_eq!(enriched.stderr.text, "real stderr");
}

#[test]
fn enrich_apply_patch_failure_output_keeps_successful_output_unchanged() {
    let output = ExecToolCallOutput {
        exit_code: 0,
        stdout: StreamOutput::new("ok".to_string()),
        stderr: StreamOutput::new(String::new()),
        aggregated_output: StreamOutput::new("ok".to_string()),
        duration: Duration::from_millis(1),
        timed_out: false,
    };

    let enriched = enrich_apply_patch_failure_output(output.clone());

    assert_eq!(enriched.stderr.text, output.stderr.text);
    assert_eq!(enriched.stdout.text, output.stdout.text);
    assert_eq!(
        enriched.aggregated_output.text,
        output.aggregated_output.text
    );
}

#[test]
fn enrich_apply_patch_failure_error_enriches_timeout_output_when_stderr_missing() {
    let output = ExecToolCallOutput {
        exit_code: 124,
        stdout: StreamOutput::new(String::new()),
        stderr: StreamOutput::new(String::new()),
        aggregated_output: StreamOutput::new("timed out while applying patch".to_string()),
        duration: Duration::from_secs(2),
        timed_out: true,
    };

    let err = ToolError::Codex(CodexErr::Sandbox(SandboxErr::Timeout {
        output: Box::new(output),
    }));

    let enriched = enrich_apply_patch_failure_error(err);

    match enriched {
        ToolError::Codex(CodexErr::Sandbox(SandboxErr::Timeout { output })) => {
            assert_eq!(output.stderr.text, "timed out while applying patch");
        }
        other => panic!("unexpected error variant: {other:?}"),
    }
}

#[test]
fn enrich_apply_patch_failure_error_enriches_denied_output_when_stderr_missing() {
    let output = ExecToolCallOutput {
        exit_code: 1,
        stdout: StreamOutput::new("stdout denied fallback".to_string()),
        stderr: StreamOutput::new("\n".to_string()),
        aggregated_output: StreamOutput::new(String::new()),
        duration: Duration::from_millis(35),
        timed_out: false,
    };

    let err = ToolError::Codex(CodexErr::Sandbox(SandboxErr::Denied {
        output: Box::new(output),
        network_policy_decision: None,
    }));

    let enriched = enrich_apply_patch_failure_error(err);

    match enriched {
        ToolError::Codex(CodexErr::Sandbox(SandboxErr::Denied { output, .. })) => {
            assert_eq!(output.stderr.text, "stdout denied fallback");
        }
        other => panic!("unexpected error variant: {other:?}"),
    }
}
