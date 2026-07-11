package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"memento-mcp/internal/feedback"
)

func feedbackSubmitToolDefinition() Tool {
	return Tool{
		Name:        "feedback_submit",
		Title:       "Submit Aggregate Helpfulness Feedback",
		Description: "Record one explicit helpful, not-helpful, or unsure signal in the opt-in local-only feedback store. Only fixed aggregate categories are accepted; no text, paths, queries, code, note content, arguments, results, or identifiers can be submitted.",
		Annotations: mutatingAnnotations(),
		InputSchema: map[string]any{
			"type":                 "object",
			"required":             []any{"rating"},
			"additionalProperties": false,
			"properties": map[string]any{
				"rating": map[string]any{
					"type": "string", "enum": []any{"helpful", "not_helpful", "unsure"},
					"description": "Explicit helpfulness signal.",
				},
				"toolCategory": map[string]any{
					"type": "string", "enum": []any{"repository", "memory", "index", "workspace", "unavailable"},
					"description": "Optional coarse category; defaults to unavailable.",
				},
				"durationBucket": map[string]any{
					"type": "string", "enum": []any{"under_100ms", "100ms_to_1s", "1s_to_10s", "10s_or_more", "unavailable"},
					"description": "Optional already-bucketed duration; defaults to unavailable.",
				},
				"resultSizeBucket": map[string]any{
					"type": "string", "enum": []any{"empty", "under_4kb", "4kb_to_32kb", "32kb_or_more", "unavailable"},
					"description": "Optional already-bucketed result size; defaults to unavailable.",
				},
				"failureClass": map[string]any{
					"type": "string", "enum": []any{"none", "tool_error", "canceled", "timeout", "unavailable"},
					"description": "Optional coarse failure class; defaults to unavailable.",
				},
			},
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"recorded": map[string]any{"type": "boolean"},
				"storage":  map[string]any{"type": "string", "const": "local-only"},
			},
			"required": []any{"recorded", "storage"},
		},
	}
}

func newFeedbackSubmitTool(recorder feedback.Recorder) Tool {
	tool := feedbackSubmitToolDefinition()
	tool.Handler = func(ctx context.Context, raw json.RawMessage) (any, error) {
		_ = ctx
		args, err := requireArgs(raw)
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownFeedbackArgs(args); err != nil {
			return nil, err
		}
		ratingText, ok := asString(args, "rating")
		if !ok {
			return nil, fmt.Errorf("rating is required")
		}
		event := feedback.Event{
			ToolCategory:     feedback.CategoryUnavailable,
			DurationBucket:   feedback.DurationUnavailable,
			ResultSizeBucket: feedback.ResultUnavailable,
			FailureClass:     feedback.FailureUnavailable,
			Rating:           feedback.Rating(ratingText),
		}
		if value, ok := asString(args, "toolCategory"); ok {
			event.ToolCategory = feedback.ToolCategory(value)
		}
		if value, ok := asString(args, "durationBucket"); ok {
			event.DurationBucket = feedback.DurationBucket(value)
		}
		if value, ok := asString(args, "resultSizeBucket"); ok {
			event.ResultSizeBucket = feedback.ResultSizeBucket(value)
		}
		if value, ok := asString(args, "failureClass"); ok {
			event.FailureClass = feedback.FailureClass(value)
		}
		event.Version = feedback.EventVersion
		if err := event.Validate(); err != nil {
			return nil, err
		}
		if err := recorder.Record(event); err != nil {
			return nil, fmt.Errorf("local feedback recording failed")
		}
		return map[string]any{"recorded": true, "storage": "local-only"}, nil
	}
	return tool
}

func rejectUnknownFeedbackArgs(args map[string]any) error {
	allowed := map[string]bool{
		"rating": true, "toolCategory": true, "durationBucket": true,
		"resultSizeBucket": true, "failureClass": true,
	}
	for key := range args {
		if !allowed[key] {
			return fmt.Errorf("unknown feedback field %q", key)
		}
	}
	return nil
}
