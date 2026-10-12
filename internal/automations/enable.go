package automations

import (
	"fmt"
	"strings"
)

// StepContent is the editable text of one step campaign.
type StepContent struct {
	Subject, Body, AltBody, PreviewText string
}

// EnableProblems returns the first reason the steps cannot be enabled (spec
// 6.4, the content rules), or "" when there is none. Steps are numbered from 1.
// The brand send guard is checked separately by the caller.
func EnableProblems(steps []StepContent) string {
	if len(steps) == 0 {
		return "The automation has no steps."
	}
	for i, s := range steps {
		for _, f := range []string{s.Subject, s.Body, s.AltBody, s.PreviewText} {
			if strings.Contains(f, Marker) {
				return fmt.Sprintf("Step %d still contains %s.", i+1, Marker)
			}
		}
	}
	return ""
}
