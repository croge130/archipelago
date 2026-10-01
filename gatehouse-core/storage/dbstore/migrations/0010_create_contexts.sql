-- Exact-match only, per Context v0: a Context identifies one specific
-- resource instance, never a class of them. No FK from grants to this
-- table yet (see Writer.CreateGrant's comment) — deferred along with
-- Context create/release operations, not required to prove the
-- evaluator's own matching logic.
CREATE TABLE IF NOT EXISTS gatehouse_contexts (
	context_type text NOT NULL REFERENCES gatehouse_context_types (type_key),
	context_id   text NOT NULL,
	lifecycle    text NOT NULL,
	PRIMARY KEY (context_type, context_id)
)
