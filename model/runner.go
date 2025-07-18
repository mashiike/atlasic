package model

import (
	"context"
	"fmt"

	"github.com/mashiike/atlasic/a2a"
)

type Runner struct {
	Model
	GenerateRequest
	ToolResolver

	lastStep *Step
}

func NewRunner(model Model, request *GenerateRequest) *Runner {
	r := &Runner{
		Model:           model,
		GenerateRequest: *request,
		ToolResolver:    &globalToolResolver,
	}
	return r
}

type Step struct {
	Index       int               `json:"index"`
	Attempt     int               `json:"attempt"`
	Response    *GenerateResponse `json:"response,omitempty"`
	ToolCalls   []*ToolUse        `json:"tool_calls,omitempty"`
	ToolResults []*ToolResult     `json:"tool_results,omitempty"`
}

type ToolUseError struct {
	ToolUse *ToolUse `json:"tool_use"`
	Err     error    `json:"error"`
}

func (e *ToolUseError) Error() string {
	return fmt.Sprintf("tool use error: %s, tool: %s", e.Err.Error(), e.ToolUse.ToolName)
}

func (e *ToolUseError) Unwrap() error {
	return e.Err
}

type ToolUseErrors []*ToolUseError

func (e ToolUseErrors) Error() string {
	if len(e) == 0 {
		return "no tool use errors"
	}
	msg := "tool use errors:\n"
	for _, err := range e {
		msg += fmt.Sprintf("- %s\n", err.Error())
	}
	return msg
}

func (e ToolUseErrors) Unwrap() []error {
	errs := make([]error, len(e))
	for i, toolUseError := range e {
		errs[i] = toolUseError
	}
	return errs
}

func (r *Runner) Next(ctx context.Context) (Step, error) {
	var currentStep Step
	if r.lastStep != nil {
		currentStep.Index = r.lastStep.Index
		currentStep.Attempt = r.lastStep.Attempt
		if r.lastStep.Response != nil {
			r.Messages = append(r.Messages, r.lastStep.Response.Message)
			toolResultMessage := a2a.Message{
				Role: a2a.RoleUser,
			}
			for _, toolResult := range r.lastStep.ToolResults {
				toolResultMessage.Parts = append(toolResultMessage.Parts, NewToolResultPart(toolResult.ID, toolResult.ToolName, toolResult.Parts))
			}
			r.Messages = append(r.Messages, toolResultMessage)
			currentStep.Index++
			currentStep.Attempt = 0
		} else {
			currentStep.Attempt++
		}
	}
	r.lastStep = &currentStep
	response, err := r.Generate(ctx, &r.GenerateRequest)
	if err != nil {
		return currentStep, err
	}
	currentStep.Response = response
	if response.StopReason != StopReasonToolUse {
		return currentStep, nil
	}
	var errs ToolUseErrors
	for _, part := range response.Message.Parts {
		toolUse, ok := AsToolUsePart(part)
		if !ok {
			continue
		}
		currentStep.ToolCalls = append(currentStep.ToolCalls, toolUse)
		handler, err := r.Resolve(ctx, toolUse)
		if err != nil {
			errs = append(errs, &ToolUseError{
				ToolUse: toolUse,
				Err:     err,
			})
			currentStep.ToolResults = append(currentStep.ToolResults, &ToolResult{
				ID:       toolUse.ID,
				ToolName: toolUse.ToolName,
				Parts: []a2a.Part{
					a2a.NewTextPart(fmt.Sprintf("can not call tool %s: %v", toolUse.ToolName, err)),
				},
			})
			continue
		}
		toolResult, err := handler(ctx, toolUse)
		if err != nil {
			errs = append(errs, &ToolUseError{
				ToolUse: toolUse,
				Err:     err,
			})
			currentStep.ToolResults = append(currentStep.ToolResults, &ToolResult{
				ID:       toolUse.ID,
				ToolName: toolUse.ToolName,
				Parts: []a2a.Part{
					a2a.NewTextPart(fmt.Sprintf("tool %s call failed: %v", toolUse.ToolName, err)),
				},
			})
			continue
		}
		toolResult.ID = toolUse.ID
		toolResult.ToolName = toolUse.ToolName
		currentStep.ToolResults = append(currentStep.ToolResults, toolResult)
	}
	if len(errs) > 0 {
		return currentStep, errs
	}
	return currentStep, nil
}

// Steps returns iter.Seq2[Step, error] that iterates over the steps of the runner.
// Example usage:
//
//	  for step, err := range runner.Steps(ctx) {
//	    if err != nil {
//	       var toolErrs ToolErrors
//	      if errors.As(err, &toolErrs) {
//	      // Handle error
//	      	continue
//		     }
//	      // Handle other errors
//		   	 break
//	    }
//	    // Process step
//	  }
func (r *Runner) Steps(ctx context.Context) func(yield func(Step, error) bool) {
	return func(yield func(Step, error) bool) {
		for {
			select {
			case <-ctx.Done():
				yield(Step{}, ctx.Err())
				return
			default:
			}

			step, err := r.Next(ctx)

			if !yield(step, err) {
				return
			}

			if step.Response != nil && step.Response.StopReason != StopReasonToolUse {
				return
			}
		}
	}
}

func (r *Runner) Response() *GenerateResponse {
	if r.lastStep == nil {
		return &GenerateResponse{
			StopReason: StopReasonError,
			Message:    a2a.Message{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("No steps executed")}},
		}
	}
	return r.lastStep.Response
}
