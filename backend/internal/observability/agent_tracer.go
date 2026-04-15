package observability

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/yourorg/sentrix/internal/provider"
)

const (
	attrFlowID    = "sentrix.flow.id"
	attrTaskID    = "sentrix.task.id"
	attrSubtaskID = "sentrix.subtask.id"
	attrAgentRole = "sentrix.agent.role"
	attrToolName  = "sentrix.tool.name"
)

// StartFlowSpan starts a top-level span for a flow execution.
func StartFlowSpan(ctx context.Context, tracer trace.Tracer, flowID uuid.UUID, title string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "flow.execute", trace.WithAttributes(
		attribute.String(attrFlowID, flowID.String()),
		attribute.String("sentrix.flow.title", title),
	))
}

// StartTaskSpan starts a span for a single task execution.
func StartTaskSpan(ctx context.Context, tracer trace.Tracer, flowID, taskID uuid.UUID, title, role string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "task.execute", trace.WithAttributes(
		attribute.String(attrFlowID, flowID.String()),
		attribute.String(attrTaskID, taskID.String()),
		attribute.String("sentrix.task.title", title),
		attribute.String(attrAgentRole, role),
	))
}

// StartAgentRunSpan starts a span for a specialist agent run.
func StartAgentRunSpan(ctx context.Context, tracer trace.Tracer, flowID, subtaskID uuid.UUID, role string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "agent.run", trace.WithAttributes(
		attribute.String(attrFlowID, flowID.String()),
		attribute.String(attrSubtaskID, subtaskID.String()),
		attribute.String(attrAgentRole, role),
	))
}

// StartToolCallSpan starts a span for a single tool invocation.
func StartToolCallSpan(ctx context.Context, tracer trace.Tracer, flowID, subtaskID uuid.UUID, role, tool string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "tool.call", trace.WithAttributes(
		attribute.String(attrFlowID, flowID.String()),
		attribute.String(attrSubtaskID, subtaskID.String()),
		attribute.String(attrAgentRole, role),
		attribute.String(attrToolName, tool),
	))
}

type tracedLLM struct {
	base   provider.LLM
	tracer trace.Tracer
}

// WrapLLM instruments LLM requests with spans and token usage attributes.
func WrapLLM(base provider.LLM, tracer trace.Tracer) provider.LLM {
	if base == nil {
		return nil
	}
	return &tracedLLM{
		base:   base,
		tracer: tracer,
	}
}

func (t *tracedLLM) Complete(
	ctx context.Context,
	messages []provider.Message,
	tools []provider.ToolDef,
	params *provider.CompletionParams,
) (*provider.Response, error) {
	start := time.Now()
	ctx, span := t.tracer.Start(ctx, "llm.complete", trace.WithAttributes(
		attribute.String("llm.provider", string(t.base.Provider())),
		attribute.String("llm.model", t.base.ModelName()),
		attribute.Int("llm.message_count", len(messages)),
		attribute.Int("llm.tool_count", len(tools)),
	))
	defer span.End()

	resp, err := t.base.Complete(ctx, messages, tools, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("llm.tokens_in", resp.TokensIn),
		attribute.Int("llm.tokens_out", resp.TokensOut),
		attribute.String("llm.finish_reason", resp.FinishReason),
		attribute.Int64("llm.duration_ms", time.Since(start).Milliseconds()),
	)
	span.SetStatus(codes.Ok, "")

	return resp, nil
}

func (t *tracedLLM) Stream(
	ctx context.Context,
	messages []provider.Message,
	tools []provider.ToolDef,
	params *provider.CompletionParams,
) (<-chan provider.StreamChunk, error) {
	ctx, span := t.tracer.Start(ctx, "llm.stream", trace.WithAttributes(
		attribute.String("llm.provider", string(t.base.Provider())),
		attribute.String("llm.model", t.base.ModelName()),
		attribute.Int("llm.message_count", len(messages)),
		attribute.Int("llm.tool_count", len(tools)),
	))

	ch, err := t.base.Stream(ctx, messages, tools, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, err
	}

	out := make(chan provider.StreamChunk, 64)
	go func() {
		defer span.End()
		defer close(out)

		chunkCount := 0
		for chunk := range ch {
			if chunk.Err != nil {
				span.RecordError(chunk.Err)
				span.SetStatus(codes.Error, chunk.Err.Error())
			}
			if chunk.Delta != "" || len(chunk.ToolCalls) > 0 {
				chunkCount++
			}
			out <- chunk
		}

		span.SetAttributes(attribute.Int("llm.stream_chunks", chunkCount))
	}()

	return out, nil
}

func (t *tracedLLM) ModelName() string {
	return t.base.ModelName()
}

func (t *tracedLLM) Provider() provider.ProviderType {
	return t.base.Provider()
}
