use codex_protocol::config_types::ReasoningSummary;
use codex_protocol::openai_models::ConfigShellToolType;
use codex_protocol::openai_models::ModelInfo;
use codex_protocol::openai_models::ModelInstructionsVariables;
use codex_protocol::openai_models::ModelMessages;
use codex_protocol::openai_models::ModelVisibility;
use codex_protocol::openai_models::TruncationMode;
use codex_protocol::openai_models::TruncationPolicyConfig;
use codex_protocol::openai_models::WebSearchToolType;
use codex_protocol::openai_models::default_input_modalities;

use crate::config::ModelsManagerConfig;
use codex_utils_output_truncation::approx_bytes_for_tokens;
use tracing::warn;

pub const BASE_INSTRUCTIONS: &str = include_str!("../prompt.md");
const DEFAULT_PERSONALITY_HEADER: &str = "You are Codex, a coding agent based on GPT-5. You and the user share the same workspace and collaborate to achieve the user's goals.";
const LOCAL_FRIENDLY_TEMPLATE: &str =
    "You optimize for team morale and being a supportive teammate as much as code quality.";
const LOCAL_PRAGMATIC_TEMPLATE: &str = "You are a deeply pragmatic, effective software engineer.";
const PERSONALITY_PLACEHOLDER: &str = "{{ personality }}";
const COMPATIBILITY_VARIANT_SUFFIXES: [&str; 4] = ["-with-tools", "-tools", "-latest", "-fast"];
const MISTRAL_LEGACY_VIBE_CLI_MODEL: &str = "mistral-vibe-cli";
const MISTRAL_CANONICAL_MODEL: &str = "mistral-vibe-cli";
const COMPATIBILITY_PROVIDER_HINTS: [&str; 11] = [
    "anthropic",
    "claude",
    "deepseek",
    "gemini",
    "grok",
    "kimi",
    "llama",
    "mistral",
    "nova",
    "openrouter",
    "qwen",
];

pub fn with_config_overrides(mut model: ModelInfo, config: &ModelsManagerConfig) -> ModelInfo {
    if let Some(supports_reasoning_summaries) = config.model_supports_reasoning_summaries
        && supports_reasoning_summaries
    {
        model.supports_reasoning_summaries = true;
    }
    if let Some(context_window) = config.model_context_window {
        model.context_window = Some(context_window);
    }
    if let Some(auto_compact_token_limit) = config.model_auto_compact_token_limit {
        model.auto_compact_token_limit = Some(auto_compact_token_limit);
    }
    if let Some(token_limit) = config.tool_output_token_limit {
        model.truncation_policy = match model.truncation_policy.mode {
            TruncationMode::Bytes => {
                let byte_limit =
                    i64::try_from(approx_bytes_for_tokens(token_limit)).unwrap_or(i64::MAX);
                TruncationPolicyConfig::bytes(byte_limit)
            }
            TruncationMode::Tokens => {
                let limit = i64::try_from(token_limit).unwrap_or(i64::MAX);
                TruncationPolicyConfig::tokens(limit)
            }
        };
    }

    if let Some(base_instructions) = &config.base_instructions {
        model.base_instructions = base_instructions.clone();
        model.model_messages = None;
    } else if !config.personality_enabled {
        model.model_messages = None;
    }

    model
}

/// Build a minimal fallback model descriptor for missing/unknown slugs.
pub fn model_info_from_slug(slug: &str) -> ModelInfo {
    warn!("Unknown model {slug} is used. This will use fallback model metadata.");
    generic_model_info_from_slug(slug, /*used_fallback_model_metadata*/ true)
}

/// Build a best-effort compatibility descriptor for common third-party
/// provider slugs so they do not fall into the user-visible fallback warning
/// path when the provider model catalog is unavailable.
pub fn compatibility_model_info_from_slug(slug: &str) -> Option<ModelInfo> {
    let normalized_slug =
        canonicalize_provider_model_slug(slug).unwrap_or_else(|| slug.to_string());
    let compatibility_base = compatibility_base_slug(normalized_slug.as_str())?;
    let mut model = generic_model_info_from_slug(
        normalized_slug.as_str(),
        /*used_fallback_model_metadata*/ false,
    );
    model.slug = slug.to_string();
    model.display_name = compatibility_display_name(compatibility_base);
    model.supports_parallel_tool_calls = slug_supports_tool_use(slug);
    model.supports_search_tool = slug_supports_tool_use(slug);
    Some(model)
}

pub fn canonicalize_provider_model_slug(slug: &str) -> Option<String> {
    canonicalize_mistral_variant_slug(slug)
}

fn generic_model_info_from_slug(slug: &str, used_fallback_model_metadata: bool) -> ModelInfo {
    ModelInfo {
        slug: slug.to_string(),
        display_name: slug.to_string(),
        description: None,
        default_reasoning_level: None,
        supported_reasoning_levels: Vec::new(),
        shell_type: ConfigShellToolType::Default,
        visibility: ModelVisibility::None,
        supported_in_api: true,
        priority: 99,
        availability_nux: None,
        upgrade: None,
        base_instructions: BASE_INSTRUCTIONS.to_string(),
        model_messages: local_personality_messages_for_slug(slug),
        supports_reasoning_summaries: false,
        default_reasoning_summary: ReasoningSummary::Auto,
        support_verbosity: false,
        default_verbosity: None,
        apply_patch_tool_type: None,
        web_search_tool_type: WebSearchToolType::Text,
        truncation_policy: TruncationPolicyConfig::bytes(/*limit*/ 10_000),
        supports_parallel_tool_calls: false,
        supports_image_detail_original: false,
        context_window: Some(272_000),
        auto_compact_token_limit: None,
        effective_context_window_percent: 95,
        experimental_supported_tools: Vec::new(),
        input_modalities: default_input_modalities(),
        used_fallback_model_metadata,
        supports_search_tool: false,
    }
}

fn compatibility_base_slug(slug: &str) -> Option<&str> {
    let terminal_segment = slug.rsplit('/').next()?;
    if terminal_segment.is_empty() {
        return None;
    }

    let variant_base = COMPATIBILITY_VARIANT_SUFFIXES
        .iter()
        .find_map(|suffix| terminal_segment.strip_suffix(suffix))
        .filter(|base| !base.is_empty());

    let candidate = variant_base.unwrap_or(terminal_segment);
    let looks_like_provider_slug = (candidate.contains('-') || candidate.contains('_'))
        && COMPATIBILITY_PROVIDER_HINTS
            .iter()
            .any(|hint| candidate.contains(hint));
    if variant_base.is_some() || looks_like_provider_slug {
        Some(candidate)
    } else {
        None
    }
}

fn compatibility_display_name(base_slug: &str) -> String {
    base_slug
        .split(['-', '_'])
        .filter(|segment| !segment.is_empty())
        .map(title_case_slug_segment)
        .collect::<Vec<_>>()
        .join(" ")
}

fn title_case_slug_segment(segment: &str) -> String {
    match segment {
        "api" => "API".to_string(),
        "cli" => "CLI".to_string(),
        "vl" => "VL".to_string(),
        other => {
            let mut chars = other.chars();
            let Some(first) = chars.next() else {
                return String::new();
            };
            let mut output = String::new();
            output.extend(first.to_uppercase());
            output.push_str(chars.as_str());
            output
        }
    }
}

fn slug_has_tool_variant(slug: &str) -> bool {
    COMPATIBILITY_VARIANT_SUFFIXES[..2]
        .iter()
        .any(|suffix| slug.ends_with(suffix))
}

fn slug_supports_tool_use(slug: &str) -> bool {
    slug_has_tool_variant(slug)
        || mistral_vibe_cli_terminal_segment(slug)
        || canonicalize_mistral_variant_slug(slug).is_some()
}

fn canonicalize_mistral_variant_slug(slug: &str) -> Option<String> {
    let (prefix, terminal_segment) = match slug.rsplit_once('/') {
        Some((prefix, terminal_segment)) => (Some(prefix), terminal_segment),
        None => (None, slug),
    };

    let canonical_terminal = if terminal_segment.eq_ignore_ascii_case(MISTRAL_CANONICAL_MODEL)
        || terminal_segment.eq_ignore_ascii_case(MISTRAL_LEGACY_VIBE_CLI_MODEL)
        || terminal_segment.eq_ignore_ascii_case("mistral-vibe-cli-with-tools")
    {
        Some(MISTRAL_CANONICAL_MODEL)
    } else {
        COMPATIBILITY_VARIANT_SUFFIXES
            .iter()
            .find_map(|suffix| terminal_segment.strip_suffix(suffix))
            .filter(|base| {
                base.eq_ignore_ascii_case(MISTRAL_CANONICAL_MODEL)
                    || base.eq_ignore_ascii_case(MISTRAL_LEGACY_VIBE_CLI_MODEL)
            })
            .map(|_| MISTRAL_CANONICAL_MODEL)
    }?;

    Some(match prefix {
        Some(prefix) => format!("{prefix}/{canonical_terminal}"),
        None => canonical_terminal.to_string(),
    })
}

fn mistral_vibe_cli_terminal_segment(slug: &str) -> bool {
    slug.rsplit('/')
        .next()
        .is_some_and(|segment| segment.eq_ignore_ascii_case(MISTRAL_LEGACY_VIBE_CLI_MODEL))
}

fn local_personality_messages_for_slug(slug: &str) -> Option<ModelMessages> {
    match slug {
        "gpt-5.2-codex" | "exp-codex-personality" => Some(ModelMessages {
            instructions_template: Some(format!(
                "{DEFAULT_PERSONALITY_HEADER}\n\n{PERSONALITY_PLACEHOLDER}\n\n{BASE_INSTRUCTIONS}"
            )),
            instructions_variables: Some(ModelInstructionsVariables {
                personality_default: Some(String::new()),
                personality_friendly: Some(LOCAL_FRIENDLY_TEMPLATE.to_string()),
                personality_pragmatic: Some(LOCAL_PRAGMATIC_TEMPLATE.to_string()),
            }),
        }),
        _ => None,
    }
}

#[cfg(test)]
#[path = "model_info_tests.rs"]
mod tests;
