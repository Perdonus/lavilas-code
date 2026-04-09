use crate::CitationStreamParser;
use crate::InlineHiddenTagParser;
use crate::InlineTagSpec;
use crate::ProposedPlanParser;
use crate::ProposedPlanSegment;
use crate::StreamTextChunk;
use crate::StreamTextParser;

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct AssistantTextChunk {
    pub visible_text: String,
    pub citations: Vec<String>,
    pub plan_segments: Vec<ProposedPlanSegment>,
}

impl AssistantTextChunk {
    pub fn is_empty(&self) -> bool {
        self.visible_text.is_empty() && self.citations.is_empty() && self.plan_segments.is_empty()
    }
}

/// Parses assistant text streaming markup in one pass:
/// - strips `<oai-mem-citation>` tags and extracts citation payloads
/// - in plan mode, also strips `<proposed_plan>` blocks and emits plan segments
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum HiddenReasoningTag {
    Think,
    Thinking,
    Thought,
}

const THINK_OPEN: &str = "<think>";
const THINK_CLOSE: &str = "</think>";
const THINKING_OPEN: &str = "<thinking>";
const THINKING_CLOSE: &str = "</thinking>";
const THOUGHT_OPEN: &str = "<thought>";
const THOUGHT_CLOSE: &str = "</thought>";

fn hidden_reasoning_parser() -> InlineHiddenTagParser<HiddenReasoningTag> {
    InlineHiddenTagParser::new(vec![
        InlineTagSpec {
            tag: HiddenReasoningTag::Think,
            open: THINK_OPEN,
            close: THINK_CLOSE,
        },
        InlineTagSpec {
            tag: HiddenReasoningTag::Thinking,
            open: THINKING_OPEN,
            close: THINKING_CLOSE,
        },
        InlineTagSpec {
            tag: HiddenReasoningTag::Thought,
            open: THOUGHT_OPEN,
            close: THOUGHT_CLOSE,
        },
    ])
}

pub fn strip_hidden_reasoning_tags(text: &str) -> String {
    let mut parser = hidden_reasoning_parser();
    let mut out = parser.push_str(text).visible_text;
    out.push_str(&parser.finish().visible_text);
    out
}

#[derive(Debug)]
pub struct AssistantTextStreamParser {
    plan_mode: bool,
    citations: CitationStreamParser,
    hidden_reasoning: InlineHiddenTagParser<HiddenReasoningTag>,
    plan: ProposedPlanParser,
}

impl AssistantTextStreamParser {
    pub fn new(plan_mode: bool) -> Self {
        Self {
            plan_mode,
            ..Self::default()
        }
    }

    pub fn push_str(&mut self, chunk: &str) -> AssistantTextChunk {
        let citation_chunk = self.citations.push_str(chunk);
        let hidden_chunk = self.hidden_reasoning.push_str(&citation_chunk.visible_text);
        let mut out = self.parse_visible_text(hidden_chunk.visible_text);
        out.citations = citation_chunk.extracted;
        out
    }

    pub fn finish(&mut self) -> AssistantTextChunk {
        let citation_chunk = self.citations.finish();
        let hidden_chunk = self.hidden_reasoning.push_str(&citation_chunk.visible_text);
        let hidden_tail = self.hidden_reasoning.finish();
        let mut visible_text = hidden_chunk.visible_text;
        visible_text.push_str(&hidden_tail.visible_text);
        let mut out = self.parse_visible_text(visible_text);
        if self.plan_mode {
            let mut tail = self.plan.finish();
            if !tail.is_empty() {
                out.visible_text.push_str(&tail.visible_text);
                out.plan_segments.append(&mut tail.extracted);
            }
        }
        out.citations = citation_chunk.extracted;
        out
    }

    fn parse_visible_text(&mut self, visible_text: String) -> AssistantTextChunk {
        if !self.plan_mode {
            return AssistantTextChunk {
                visible_text,
                ..AssistantTextChunk::default()
            };
        }
        let plan_chunk: StreamTextChunk<ProposedPlanSegment> = self.plan.push_str(&visible_text);
        AssistantTextChunk {
            visible_text: plan_chunk.visible_text,
            plan_segments: plan_chunk.extracted,
            ..AssistantTextChunk::default()
        }
    }
}

impl Default for AssistantTextStreamParser {
    fn default() -> Self {
        Self {
            plan_mode: false,
            citations: CitationStreamParser::default(),
            hidden_reasoning: hidden_reasoning_parser(),
            plan: ProposedPlanParser::default(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::AssistantTextStreamParser;
    use super::strip_hidden_reasoning_tags;
    use crate::ProposedPlanSegment;
    use pretty_assertions::assert_eq;

    #[test]
    fn parses_citations_across_seed_and_delta_boundaries() {
        let mut parser = AssistantTextStreamParser::new(/*plan_mode*/ false);

        let seeded = parser.push_str("hello <oai-mem-citation>doc");
        let parsed = parser.push_str("1</oai-mem-citation> world");
        let tail = parser.finish();

        assert_eq!(seeded.visible_text, "hello ");
        assert_eq!(seeded.citations, Vec::<String>::new());
        assert_eq!(parsed.visible_text, " world");
        assert_eq!(parsed.citations, vec!["doc1".to_string()]);
        assert_eq!(tail.visible_text, "");
        assert_eq!(tail.citations, Vec::<String>::new());
    }

    #[test]
    fn parses_plan_segments_after_citation_stripping() {
        let mut parser = AssistantTextStreamParser::new(/*plan_mode*/ true);

        let seeded = parser.push_str("Intro\n<proposed");
        let parsed = parser.push_str("_plan>\n- step <oai-mem-citation>doc</oai-mem-citation>\n");
        let tail = parser.push_str("</proposed_plan>\nOutro");
        let finish = parser.finish();

        assert_eq!(seeded.visible_text, "Intro\n");
        assert_eq!(
            seeded.plan_segments,
            vec![ProposedPlanSegment::Normal("Intro\n".to_string())]
        );
        assert_eq!(parsed.visible_text, "");
        assert_eq!(parsed.citations, vec!["doc".to_string()]);
        assert_eq!(
            parsed.plan_segments,
            vec![
                ProposedPlanSegment::ProposedPlanStart,
                ProposedPlanSegment::ProposedPlanDelta("- step \n".to_string()),
            ]
        );
        assert_eq!(tail.visible_text, "Outro");
        assert_eq!(
            tail.plan_segments,
            vec![
                ProposedPlanSegment::ProposedPlanEnd,
                ProposedPlanSegment::Normal("Outro".to_string()),
            ]
        );
        assert!(finish.is_empty());
    }

    #[test]
    fn strips_hidden_reasoning_tags_across_chunk_boundaries() {
        let mut parser = AssistantTextStreamParser::new(/*plan_mode*/ false);

        let seeded = parser.push_str("Visible <tho");
        let parsed = parser.push_str("ught>private</thought> text");
        let tail = parser.finish();

        assert_eq!(seeded.visible_text, "Visible ");
        assert_eq!(parsed.visible_text, " text");
        assert_eq!(tail.visible_text, "");
    }

    #[test]
    fn strip_hidden_reasoning_tags_handles_think_variants() {
        assert_eq!(
            strip_hidden_reasoning_tags("A<think>hidden</think>B<thinking>more</thinking>C"),
            "ABC"
        );
    }
}
