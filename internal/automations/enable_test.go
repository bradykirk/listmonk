package automations

import "testing"

func TestEnableProblems(t *testing.T) {
	ok := []StepContent{{Subject: "Welcome", Body: "<p>Hi</p>"}}
	if p := EnableProblems(ok); p != "" {
		t.Fatalf("clean: %q", p)
	}
	if p := EnableProblems(nil); p != "The automation has no steps." {
		t.Fatalf("no steps: %q", p)
	}
	bad := []StepContent{{Subject: "Welcome", Body: "<p>[[SENDER_INFO]]</p>"}}
	if p := EnableProblems(bad); p != "Step 1 still contains [[SENDER_INFO]]." {
		t.Fatalf("marker: %q", p)
	}
	pv := []StepContent{{Subject: "W", PreviewText: "[[SENDER_INFO]]"}}
	if p := EnableProblems(pv); p != "Step 1 still contains [[SENDER_INFO]]." {
		t.Fatalf("preview: %q", p)
	}
	alt := []StepContent{{Subject: "W", Body: "ok"}, {Subject: "W", AltBody: "x [[SENDER_INFO]]"}}
	if p := EnableProblems(alt); p != "Step 2 still contains [[SENDER_INFO]]." {
		t.Fatalf("alt body step 2: %q", p)
	}
	subj := []StepContent{{Subject: "[[SENDER_INFO]]"}}
	if p := EnableProblems(subj); p != "Step 1 still contains [[SENDER_INFO]]." {
		t.Fatalf("subject: %q", p)
	}
}
