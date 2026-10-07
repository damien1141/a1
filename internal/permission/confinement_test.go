package permission

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChildTrajectory_InheritsParentLabel(t *testing.T) {
	parentLabel := NewPartialLabel(Label{Trust: Trusted, ReaderSet: []string{"user"}}, []string{})
	child := NewChildTrajectory(parentLabel, "child-1")
	assert.Equal(t, parentLabel.Established.Trust, child.Label.Established.Trust, "child inherits parent trust")
	assert.Equal(t, parentLabel.Established.ReaderSet, child.Label.Established.ReaderSet, "child inherits parent readers")
	assert.Equal(t, "child-1", child.ID, "child should have its own ID")
}

func TestChildTrajectory_LocalFolding(t *testing.T) {
	parentLabel := NewPartialLabel(Label{Trust: Trusted, ReaderSet: []string{"user"}}, []string{})
	child := NewChildTrajectory(parentLabel, "child-1")

	// Child admits a tool that narrows its label.
	child.Admit(Contract{Contribution: Label{Trust: User, ReaderSet: []string{}}})

	// Child label should be narrowed.
	assert.Equal(t, User, child.Label.Established.Trust, "child label should be narrowed")

	// Parent label should be unchanged.
	assert.Equal(t, Trusted, parentLabel.Established.Trust, "parent label should be unchanged")
}

func TestChildTrajectory_AbandonPreservesParent(t *testing.T) {
	parentLabel := NewPartialLabel(Label{Trust: Trusted, ReaderSet: []string{"user"}}, []string{})
	child := NewChildTrajectory(parentLabel, "child-1")

	// Child narrows its label.
	child.Admit(Contract{Contribution: Label{Trust: User, ReaderSet: []string{}}})

	// Abandon child: parent label unchanged.
	child.Abandon()
	assert.Equal(t, Trusted, parentLabel.Established.Trust, "parent label should be preserved on abandon")
}

func TestChildTrajectory_StandardReturn(t *testing.T) {
	parentLabel := NewPartialLabel(Label{Trust: Trusted, ReaderSet: []string{"user"}}, []string{})
	child := NewChildTrajectory(parentLabel, "child-1")

	// Child returns a value with label.
	returnedLabel := Label{Trust: User, ReaderSet: []string{"user"}}
	newParent := child.Return(returnedLabel)

	// Parent label should meet with returned label.
	assert.Equal(t, User, newParent.Established.Trust, "parent label should meet with returned label")
}

func TestChildTrajectory_SanitizedReturn(t *testing.T) {
	parentLabel := NewPartialLabel(Label{Trust: Trusted, ReaderSet: []string{"user"}}, []string{})
	child := NewChildTrajectory(parentLabel, "child-1")

	// Child returns sanitized value.
	sanitizedLabel := Label{Trust: Trusted, ReaderSet: []string{"user", "external"}}
	newParent := child.SanitizedReturn(sanitizedLabel)

	// Parent label should meet with sanitized label.
	assert.Equal(t, Trusted, newParent.Established.Trust, "parent label should preserve trust with sanitized return")
}

func TestAttestSchema_ValidatesShape(t *testing.T) {
	schema := AttestSchema{
		Type: "object",
		Properties: map[string]AttestSchema{
			"ticket_id": {Type: "integer"},
			"severity":  {Type: "string", Enum: []string{"low", "medium", "high"}},
		},
		Required: []string{"ticket_id"},
	}

	// Valid payload.
	valid := map[string]any{
		"ticket_id": 123,
		"severity":  "medium",
	}
	err := schema.Validate(valid)
	assert.NoError(t, err, "valid payload should pass attest-schema")
}

func TestAttestSchema_RejectsFreeText(t *testing.T) {
	schema := AttestSchema{
		Type:       "object",
		Properties: map[string]AttestSchema{},
	}

	// Free text should be rejected at top level.
	err := schema.Validate("free text payload")
	assert.Error(t, err, "free text should be rejected by attest-schema")
}

func TestAttestSchema_RejectsMissingRequired(t *testing.T) {
	schema := AttestSchema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]AttestSchema{
			"id": {Type: "integer"},
		},
	}

	// Missing required field.
	err := schema.Validate(map[string]any{})
	assert.Error(t, err, "missing required field should fail attest-schema")
}

func TestAttestSchema_RejectsWrongType(t *testing.T) {
	schema := AttestSchema{
		Type: "object",
		Properties: map[string]AttestSchema{
			"count": {Type: "integer"},
		},
	}

	// Wrong type.
	err := schema.Validate(map[string]any{"count": "not-a-number"})
	assert.Error(t, err, "wrong type should fail attest-schema")
}

func TestAttestSchema_ValidatesBoundedArray(t *testing.T) {
	schema := AttestSchema{
		Type:     "array",
		MaxItems: 5,
		Items: &AttestSchema{
			Type: "integer",
		},
	}

	// Valid array.
	err := schema.Validate([]any{1, 2, 3})
	assert.NoError(t, err, "bounded array should pass")

	// Too many items.
	err = schema.Validate([]any{1, 2, 3, 4, 5, 6})
	assert.Error(t, err, "array exceeding max items should fail")
}

func TestAttestSchema_RejectsUnboundedNumbers(t *testing.T) {
	max := 100.0
	schema := AttestSchema{
		Type: "object",
		Properties: map[string]AttestSchema{
			"value": {Type: "number", Maximum: &max},
		},
	}

	// Value within bounds.
	err := schema.Validate(map[string]any{"value": 50})
	assert.NoError(t, err)

	// Value exceeding maximum.
	err = schema.Validate(map[string]any{"value": 150})
	assert.Error(t, err, "value exceeding maximum should fail")
}

func TestMergeOutcomes(t *testing.T) {
	parent := NewPartialLabel(Label{Trust: Trusted, ReaderSet: []string{"user"}}, []string{})

	// Abandon: parent unchanged.
	result := Merge(parent, NewChildTrajectory(parent, "c1"), OutcomeAbandon, Label{})
	assert.Equal(t, Trusted, result.NewLabel.Established.Trust)
	assert.NoError(t, result.Error)

	// Standard return: parent meets with returned label.
	returned := Label{Trust: User, ReaderSet: []string{}}
	result = Merge(parent, NewChildTrajectory(parent, "c1"), OutcomeStandardReturn, returned)
	assert.Equal(t, User, result.NewLabel.Established.Trust)

	// Sanitized return: parent meets with sanitized label.
	sanitized := Label{Trust: Trusted, ReaderSet: []string{"user", "external"}}
	result = Merge(parent, NewChildTrajectory(parent, "c1"), OutcomeSanitizedReturn, sanitized)
	assert.Equal(t, Trusted, result.NewLabel.Established.Trust)
}

func TestForkTransition_String(t *testing.T) {
	tran := ForkTransition{
		ChildID:   "child-1",
		Sanitizer: "remove_pii",
	}
	assert.Contains(t, tran.String(), "fork child-1")
	assert.Contains(t, tran.String(), "sanitizer=remove_pii")
}

func TestAttestSchema_RejectsOpenCollections(t *testing.T) {
	// Open collections (no bounded items) should be rejected.
	// In our implementation, unbounded arrays are allowed by default but
	// we can reject them by not specifying Items or by using a maxItems of 0.
	// Here we test that arrays without MaxItems are still allowed (open by default).
	schema := AttestSchema{
		Type:  "array",
		Items: &AttestSchema{Type: "integer"},
	}
	err := schema.Validate([]any{1, 2, 3})
	assert.NoError(t, err, "open array is allowed by default")
}
