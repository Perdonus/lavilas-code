use super::*;
use crate::ModelsManagerConfig;
use pretty_assertions::assert_eq;

#[test]
fn reasoning_summaries_override_true_enables_support() {
    let model = model_info_from_slug("unknown-model");
    let config = ModelsManagerConfig {
        model_supports_reasoning_summaries: Some(true),
        ..Default::default()
    };

    let updated = with_config_overrides(model.clone(), &config);
    let mut expected = model;
    expected.supports_reasoning_summaries = true;

    assert_eq!(updated, expected);
}

#[test]
fn reasoning_summaries_override_false_does_not_disable_support() {
    let mut model = model_info_from_slug("unknown-model");
    model.supports_reasoning_summaries = true;
    let config = ModelsManagerConfig {
        model_supports_reasoning_summaries: Some(false),
        ..Default::default()
    };

    let updated = with_config_overrides(model.clone(), &config);

    assert_eq!(updated, model);
}

#[test]
fn reasoning_summaries_override_false_is_noop_when_model_is_false() {
    let model = model_info_from_slug("unknown-model");
    let config = ModelsManagerConfig {
        model_supports_reasoning_summaries: Some(false),
        ..Default::default()
    };

    let updated = with_config_overrides(model.clone(), &config);

    assert_eq!(updated, model);
}

#[test]
fn canonicalize_provider_model_slug_repairs_mistral_tool_variant() {
    assert_eq!(
        canonicalize_provider_model_slug("mistral-vibe-cli-with-tools"),
        Some("mistral-vibe-cli".to_string())
    );
    assert_eq!(
        canonicalize_provider_model_slug("mistral-vibe-cli-fast"),
        Some("mistral-vibe-cli".to_string())
    );
    assert_eq!(
        canonicalize_provider_model_slug("mistral/mistral-vibe-cli-with-tools"),
        Some("mistral/mistral-vibe-cli".to_string())
    );
}

#[test]
fn compatibility_model_info_keeps_tool_support_for_canonical_mistral_vibe_cli() {
    let model = compatibility_model_info_from_slug("mistral-vibe-cli")
        .expect("canonical Mistral Vibe CLI should produce compatibility metadata");

    assert_eq!(model.slug, "mistral-vibe-cli");
    assert_eq!(model.display_name, "Mistral Vibe CLI");
    assert!(model.supports_parallel_tool_calls);
    assert!(model.supports_search_tool);
    assert!(!model.used_fallback_model_metadata);
}

#[test]
fn compatibility_model_info_repairs_mistral_fast_variant() {
    let model = compatibility_model_info_from_slug("mistral-vibe-cli-fast")
        .expect("Mistral fast alias should produce compatibility metadata");

    assert_eq!(model.slug, "mistral-vibe-cli");
    assert!(model.supports_parallel_tool_calls);
    assert!(model.supports_search_tool);
}
