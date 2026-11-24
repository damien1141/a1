package permission

// Policy.Unrestricted reports whether this policy promises never to hold a
// call back for approval — the policy-level twin of Unrestricted(gate), and
// the one predicate every "is approval gone?" decision should consult.
//
// It exists because the two sides of the bash sandbox used to answer that
// question with different predicates. The gate side suppressed containment on
// Mode == ModeYolo alone; the exec side (bashtool.bashSandboxProfile) read the
// live mode off the context. Both missed the allow-all flags and the armed
// session override — shapes whose entire purpose is "stop asking me" — so a
// run with dangerously_allow_all: true kept its walls on BOTH sides while no
// prompt could ever justify them. One predicate, both sides, no drift.
func (p Policy) Unrestricted() bool {
	return p.Mode == ModeYolo || p.DangerouslyAllowAll || p.AllowAllSession
}
