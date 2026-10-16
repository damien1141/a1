package permission

import (
	"fmt"
	"slices"
	"strings"
)

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	case int32:
		return float64(val), true
	case uint32:
		return float64(val), true
	case float32:
		return float64(val), true
	default:
		return 0, false
	}
}

// ChildTrajectory represents a disposable child branch spawned from a parent
// trajectory. The child inherits the parent's label at fork time (L0c := Lp)
// and accumulates label descent locally. External effects committed by the child
// remain in the shared log E, but the parent label is only modified by an
// explicit merge (APPA §6, Theorem 8).
type ChildTrajectory struct {
	// ID is the unique child branch identifier.
	ID string
	// Label is the child's current trajectory label, initialized from parent.
	Label PartialLabel
	// Effects is the child's view of the shared effect log E.
	Effects *EffectLog
	// ParentLabel is the parent's label at fork time (for merge reference).
	ParentLabel PartialLabel
	// Schema is the shape-bounded return channel fixed at fork time.
	Schema AttestSchema
}

// NewChildTrajectory creates a child trajectory with inherited label. The
// parent's effect log is shared, not copied: a child abandoning a branch still
// leaves its committed effects recorded, so no_prior checks cannot be evaded.
func NewChildTrajectory(parentLabel PartialLabel, id string, schema AttestSchema, shared *EffectLog) *ChildTrajectory {
	effects := shared
	if effects == nil {
		effects = NewEffectLog()
	}
	return &ChildTrajectory{
		ID:          id,
		Label:       parentLabel,
		Effects:     effects,
		ParentLabel: parentLabel,
		Schema:      schema,
	}
}

// Admit folds a tool contribution into the child's local label.
func (c *ChildTrajectory) Admit(contract Contract) {
	c.Label = c.Label.Meet(PartialLabel{
		Established: contract.Contribution,
		Unresolved:  []string{},
	})
	for _, token := range contract.EffectTokens {
		c.Effects.Commit("child", token)
	}
}

// Abandon discards the child trajectory without modifying the parent.
func (*ChildTrajectory) Abandon() {
	// No-op: parent label is unchanged.
}

// Return performs a standard labeled return: the child returns value v
// via submit_result(v). If v narrows the parent, it triggers an ordinary
// narrowing check requiring explicit parent acceptance.
//
// The returned label is the candidate merge label, not the new parent label:
// Merge decides whether a narrowing check is required.
func (*ChildTrajectory) Return(v Label) PartialLabel {
	return PartialLabel{
		Established: v,
		Unresolved:  []string{},
	}
}

// SanitizedReturn performs a sanitized or schema-attested return: a registered
// semantic sanitizer or structural schema check produces a representation whose
// declared TCB mandate determines its merge label.
//
// The value is validated against the attest-schema fixed at fork time. A value
// that fails the schema cannot leave the branch, because the shape-bounded
// channel is the only exit.
func (c *ChildTrajectory) SanitizedReturn(value any, mandate Label) (PartialLabel, error) {
	if err := c.Schema.Validate(value); err != nil {
		return PartialLabel{}, fmt.Errorf("attest-schema rejected child return: %w", err)
	}
	return PartialLabel{
		Established: mandate,
		Unresolved:  []string{},
	}, nil
}

// AttestSchemaValidator validates that a value conforms to a shape-bounded
// return schema. The dialect admits booleans, bounded integers, fixed-precision
// decimals, closed enums/formats, and bounded arrays/objects, while rejecting
// free text, unbounded numbers, and open collections.
type AttestSchemaValidator struct {
	Schema AttestSchema
}

// AttestSchema describes a shape-bounded return schema.
type AttestSchema struct {
	// Type is the JSON Schema type: "boolean", "integer", "number", "string",
	// "array", "object".
	Type string
	// Enum is a closed set of allowed string values.
	Enum []string
	// Format is a closed format: "date", "email", "uri", etc.
	Format string
	// Maximum is the inclusive upper bound for integer/number types.
	Maximum *float64
	// MinItems is the minimum array length.
	MinItems int
	// MaxItems is the maximum array length (bounded array).
	MaxItems int
	// Items describes the schema for array elements.
	Items *AttestSchema
	// Properties describes the schema for object properties.
	Properties map[string]AttestSchema
	// Required lists required object property names.
	Required []string
}

// Validate checks whether value conforms to the schema.
func (s AttestSchema) Validate(value any) error {
	return s.validate(value, "")
}

func (s AttestSchema) validate(value any, path string) error {
	switch s.Type {
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean at %s, got %T", path, value)
		}
		return nil

	case "integer":
		f, ok := toFloat64(value)
		if !ok {
			return fmt.Errorf("expected integer at %s, got %T", path, value)
		}
		if f != float64(int64(f)) {
			return fmt.Errorf("expected integer at %s, got float with fractional part", path)
		}
		if s.Maximum != nil && f > *s.Maximum {
			return fmt.Errorf("value %v exceeds maximum %v at %s", f, *s.Maximum, path)
		}
		return nil

	case "number":
		f, ok := toFloat64(value)
		if !ok {
			return fmt.Errorf("expected number at %s, got %T", path, value)
		}
		if s.Maximum != nil && f > *s.Maximum {
			return fmt.Errorf("value %v exceeds maximum %v at %s", f, *s.Maximum, path)
		}
		return nil

	case "string":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string at %s, got %T", path, value)
		}
		if len(s.Enum) > 0 && !slices.Contains(s.Enum, str) {
			return fmt.Errorf("string %q not in allowed enum %v at %s", str, s.Enum, path)
		}
		return nil

	case "array":
		arr, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array at %s, got %T", path, value)
		}
		if s.MaxItems > 0 && len(arr) > s.MaxItems {
			return fmt.Errorf("array length %d exceeds max %d at %s", len(arr), s.MaxItems, path)
		}
		if s.MinItems > 0 && len(arr) < s.MinItems {
			return fmt.Errorf("array length %d below min %d at %s", len(arr), s.MinItems, path)
		}
		if s.Items != nil {
			for i, item := range arr {
				if err := s.Items.validate(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
		return nil

	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object at %s, got %T", path, value)
		}
		for _, req := range s.Required {
			if _, ok := obj[req]; !ok {
				return fmt.Errorf("missing required property %q at %s", req, path)
			}
		}
		for name, propSchema := range s.Properties {
			if v, ok := obj[name]; ok {
				if err := propSchema.validate(v, fmt.Sprintf("%s.%s", path, name)); err != nil {
					return err
				}
			}
		}
		return nil

	case "null", "":
		// Null type or empty type is not allowed in attest-schema.
		return fmt.Errorf("type %q is not allowed in attest-schema at %s", s.Type, path)

	default:
		return fmt.Errorf("unknown attest-schema type %q at %s", s.Type, path)
	}
}

// ForkTransition represents spawning a child branch from the parent trajectory.
type ForkTransition struct {
	// ChildID is the unique child branch identifier.
	ChildID string
	// Schema is the attest-schema for the child's return channel.
	Schema AttestSchema
	// Sanitizer is an optional registered sanitizer to apply before return.
	Sanitizer string
}

// String returns a human-readable fork transition description.
func (t ForkTransition) String() string {
	var parts []string
	parts = append(parts, "fork "+t.ChildID)
	if t.Sanitizer != "" {
		parts = append(parts, "sanitizer="+t.Sanitizer)
	}
	return strings.Join(parts, " ")
}

// Outcome represents the result of a child branch execution.
type Outcome string

const (
	// OutcomeAbandon means the child was discarded, no parent transition.
	OutcomeAbandon Outcome = "abandon"
	// OutcomeStandardReturn means the child returned a labeled value.
	OutcomeStandardReturn Outcome = "standard_return"
	// OutcomeSanitizedReturn means the child returned a sanitized value.
	OutcomeSanitizedReturn Outcome = "sanitized_return"
)

// MergeResult describes the parent state after merging a child outcome.
type MergeResult struct {
	// Outcome is how the child terminated.
	Outcome Outcome
	// NewLabel is the parent's new label after merge.
	NewLabel PartialLabel
	// RequiresAcceptance is true when the return narrows the parent label,
	// which needs explicit acceptance before the parent dispatches against
	// the merged label (APPA §6 exit rule 2).
	RequiresAcceptance bool
	// Effects are the effects committed by the child.
	Effects []string
	// Error is non-nil if the merge failed.
	Error error
}

// Merge incorporates a child's outcome into the parent trajectory, per APPA §6.
// Abandonment leaves the parent label untouched; a standard return narrows the
// parent (L′p = Lp ∧ label(v)); a sanitized return folds the attested mandate.
func Merge(parent PartialLabel, child *ChildTrajectory, outcome Outcome, value Label) MergeResult {
	switch outcome {
	case OutcomeAbandon:
		return MergeResult{
			Outcome:  OutcomeAbandon,
			NewLabel: parent,
			Effects:  child.Effects.Support(),
		}

	case OutcomeStandardReturn, OutcomeSanitizedReturn:
		returned := PartialLabel{Established: value, Unresolved: []string{}}
		newLabel := parent.Meet(returned)
		return MergeResult{
			Outcome:            outcome,
			NewLabel:           newLabel,
			RequiresAcceptance: IsNarrower(parent.Established, newLabel.Established),
			Effects:            child.Effects.Support(),
		}

	default:
		return MergeResult{
			Outcome:  outcome,
			NewLabel: parent,
			Error:    fmt.Errorf("unknown child outcome: %s", outcome),
		}
	}
}
