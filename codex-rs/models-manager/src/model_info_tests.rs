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
        Some("mistral-medium-latest".to_string())
    );
    assert_eq!(
        canonicalize_provider_model_slug("mistral-vibe-cli-fast"),
        Some("mistral-small-latest".to_string())
    );
    assert_eq!(
        canonicalize_provider_model_slug("mistral/mistral-vibe-cli-with-tools"),
        Some("mistral/mistral-medium-latest".to_string())
    );
}

#[test]
fn normalize_provider_model_alias_slug_repairs_gemini_legacy_aliases() {
    assert_eq!(
        normalize_provider_model_alias_slug("gemini-flash-latest"),
        Some("gemini-2.5-flash".to_string())
    );
    assert_eq!(
        normalize_provider_model_alias_slug("gemini-flash-lite-latest"),
        Some("gemini-2.5-flash-lite".to_string())
    );
    assert_eq!(
        normalize_provider_model_alias_slug("models/gemini-pro-latest"),
        Some("models/gemini-2.5-pro".to_string())
    );
}

#[test]
fn compatibility_model_info_keeps_tool_support_for_canonical_mistral_vibe_cli() {
    let model = compatibility_model_info_from_slug("mistral-vibe-cli")
        .expect("canonical Mistral Vibe CLI should produce compatibility metadata");

    assert_eq!(model.slug, "mistral-vibe-cli");
    assert_eq!(model.display_name, "Mistral Medium");
    assert!(model.supports_parallel_tool_calls);
    assert!(model.supports_search_tool);
    assert!(!model.used_fallback_model_metadata);
}

#[test]
fn compatibility_model_info_repairs_mistral_fast_variant() {
    let model = compatibility_model_info_from_slug("mistral-vibe-cli-fast")
        .expect("Mistral fast alias should produce compatibility metadata");

    assert_eq!(model.slug, "mistral-vibe-cli-fast");
    assert_eq!(model.display_name, "Mistral Small");
    assert!(model.supports_parallel_tool_calls);
    assert!(model.supports_search_tool);
}

#[test]
fn compatibility_model_info_supports_real_mistral_families() {
    let devstral = compatibility_model_info_from_slug("devstral-latest")
        .expect("devstral should produce compatibility metadata");
    assert_eq!(devstral.slug, "devstral-latest");
    assert_eq!(devstral.display_name, "Devstral");
    assert!(devstral.supports_parallel_tool_calls);
    assert!(devstral.supports_search_tool);
    assert!(!devstral.used_fallback_model_metadata);

    let pixtral = compatibility_model_info_from_slug("pixtral-large-latest")
        .expect("pixtral should produce compatibility metadata");
    assert_eq!(pixtral.slug, "pixtral-large-latest");
    assert_eq!(pixtral.display_name, "Pixtral Large");
    assert!(pixtral.supports_parallel_tool_calls);
    assert!(pixtral.supports_search_tool);
    assert!(!pixtral.used_fallback_model_metadata);
}
