package preprocessor

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
)

// Request is the mutable input passed through a Pipeline. Steps may
// modify UserInput and append to SystemPromptAddition. The agent layer
// is responsible for translating Request back onto its turn state.
//
// Request is intentionally narrow: it does not expose the full Turn
// object so that pipeline steps remain easy to test in isolation and
// safe to reorder.
type Request struct {
	// UserInput is the (possibly already preprocessed) user message.
	UserInput string

	// SystemPromptAddition accumulates extra system-prompt fragments
	// that downstream steps want to inject. Steps should append to
	// this field rather than overwriting it.
	SystemPromptAddition string

	// WorkingDir is the agent's resolved working directory.
	WorkingDir string

	// Mailbox is the current agent mailbox, when mailbox input should be
	// drained and attached to the turn input.
	Mailbox mailbox.Mailbox

	// Metadata carries optional step-specific data (e.g. detected
	// command type) so callers can react without re-parsing the
	// input. Steps must not assume keys exist.
	Metadata map[string]string
}

// SetMetadata sets a metadata key, allocating the map on first use.
func (r *Request) SetMetadata(key, value string) {
	if r.Metadata == nil {
		r.Metadata = make(map[string]string)
	}
	r.Metadata[key] = value
}

// Step is a single preprocessing stage. Steps should be cheap, side-effect
// free outside of mutating the Request, and tolerant of disabled features
// (returning the request unchanged).
//
// A Step that returns an error aborts the pipeline; the partially-mutated
// Request is still returned so callers can decide how to recover.
type Step interface {
	Name() string
	Apply(ctx context.Context, req *Request) error
}

// StepFunc adapts a plain function to the Step interface.
type StepFunc struct {
	StepName string
	Fn       func(ctx context.Context, req *Request) error
}

// Name returns the step's display name.
func (s StepFunc) Name() string { return s.StepName }

// Apply runs the underlying function.
func (s StepFunc) Apply(ctx context.Context, req *Request) error {
	if s.Fn == nil {
		return nil
	}
	return s.Fn(ctx, req)
}

// Pipeline runs a sequence of preprocessing Steps in order.
//
// Pipeline is the unified entry point for preprocessing concerns
// (mailbox, OpenSpec, Skill, Routine, ...). Existing per-feature helpers
// in this package can be wrapped as Steps, allowing the agent to wire
// them via a single Run() call instead of several conditional branches.
type Pipeline struct {
	steps []Step
}

// NewPipeline constructs a pipeline with the given ordered steps.
func NewPipeline(steps ...Step) *Pipeline {
	return &Pipeline{steps: append([]Step(nil), steps...)}
}

// Append adds a step to the end of the pipeline and returns the pipeline
// to support fluent construction.
func (p *Pipeline) Append(step Step) *Pipeline {
	if step != nil {
		p.steps = append(p.steps, step)
	}
	return p
}

// Steps returns a copy of the configured steps. Useful for tests and
// debug logging.
func (p *Pipeline) Steps() []Step {
	out := make([]Step, len(p.steps))
	copy(out, p.steps)
	return out
}

// Run executes every step in order against req. The first error short-
// circuits remaining steps; the returned error is wrapped with the
// failing step's name for easier diagnosis.
func (p *Pipeline) Run(ctx context.Context, req *Request) error {
	if req == nil {
		return fmt.Errorf("preprocessor: nil request")
	}
	for _, step := range p.steps {
		if step == nil {
			continue
		}
		if err := step.Apply(ctx, req); err != nil {
			return fmt.Errorf("preprocessor step %q: %w", step.Name(), err)
		}
	}
	return nil
}

// RoutinesStep rewrites /routines slash commands using the existing
// RewriteRoutinesCommand helper. It is provided as the simplest concrete
// Step so callers can adopt the pipeline incrementally.
func RoutinesStep() Step {
	return StepFunc{
		StepName: "routines",
		Fn: func(_ context.Context, req *Request) error {
			if rewritten, ok := RewriteRoutinesCommand(req.UserInput); ok {
				req.UserInput = rewritten
				req.SetMetadata("routines.rewritten", "true")
			}
			return nil
		},
	}
}

// MailboxStep drains request.Mailbox and appends any formatted attachment to
// UserInput. It preserves the previous Turn.Execute behavior while moving the
// mailbox concern behind the shared preprocessing pipeline abstraction.
func MailboxStep() Step {
	return StepFunc{
		StepName: "mailbox",
		Fn: func(ctx context.Context, req *Request) error {
			attachment, ok, err := DrainMailboxAttachment(ctx, req.Mailbox)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			req.UserInput += "\n\n" + attachment
			req.SetMetadata("mailbox.drained", "true")
			return nil
		},
	}
}

// OpenSpecStep wraps ProcessOpenSpecCommand as a pipeline Step. The
// supplied opts are evaluated lazily via the optsFn so callers can defer
// reading config until pipeline execution time.
func OpenSpecStep(optsFn func() OpenSpecOptions) Step {
	return StepFunc{
		StepName: "openspec",
		Fn: func(_ context.Context, req *Request) error {
			if optsFn == nil {
				return nil
			}
			opts := optsFn()
			if !opts.Enabled {
				return nil
			}
			if opts.WorkingDir == "" {
				opts.WorkingDir = req.WorkingDir
			}
			result := ProcessOpenSpecCommand(req.UserInput, opts)
			if !result.Handled {
				return nil
			}
			req.UserInput = result.UserInput
			if result.SystemPromptAddition != "" {
				req.SystemPromptAddition += result.SystemPromptAddition
			}
			req.SetMetadata("openspec.command", result.CommandType)
			req.SetMetadata("openspec.change", result.ChangeName)
			// Surface the underlying handler error to callers without
			// aborting the pipeline: ProcessOpenSpecCommand already
			// rewrote UserInput to an error explanation in that case.
			return nil
		},
	}
}
