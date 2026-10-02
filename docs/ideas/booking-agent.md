# Booking agent: natural-language requests with reliable execution

**Status:** Future experiment; unscheduled and not implemented.  
**Recorded:** 2026-10-02 — discussion with Nancy Zhang.

## The idea

Add a small agent interface to Alloca-Go so a user can ask:

> Find me an available slot tomorrow afternoon and reserve the one I choose.

The agent interprets the request, discovers candidate slots, asks for clarification
or a choice when needed, and invokes structured Alloca-Go operations. Alloca-Go
continues to decide capacity, schedule conflicts, reservation validity and
idempotent outcomes through its existing service and database authority.

This connects the current database/distributed-systems study with future AI
application and agent-runtime learning. Model computation is one component;
task state, tool execution and recovery are central engineering questions.

## Authority and meaning

The model proposes actions; the booking service enforces the rules.

- Convert relative dates such as "tomorrow" into explicit dates and time zones
  before submitting a mutation. Ask about consequential ambiguity.
- Use structured tool arguments and service-assigned identifiers.
- Availability reads suggest candidates; the mutation makes the authoritative
  decision under concurrency.
- Distinguish an expiring reservation hold from a confirmed booking. A successful
  reserve must not be described as a confirmed booking.
- Report service refusals and unresolved outcomes accurately; a timeout is not
  proof that the operation failed.

The existing [transaction semantics](../design/transaction-semantics.md) remain
the authority. Agent-generated text does not change those semantics.

## Small first scope

Use a local environment, synthetic slots and one fixed test identity. The current
identity fields are caller-supplied and do not provide authentication.

Start with a Python agent loop and a narrow tool adapter to the existing Go HTTP
API: list candidate slots and submit a reservation after the user's choice.
Audit the current API before implementation; any missing read/reconciliation
capability is a proposed extension, not an existing feature assumed by this note.

Keep the first experiment on one supported writable authority. Cross-authority
booking is a separate [future protocol exploration](cross-authority-booking.md).

Persist the selected intent, validated mutation payload, operation key and
execution progress. Give each intentional mutation one stable idempotency key
in Alloca-Go's existing scope. Persist the key and payload before sending the
request, and reuse both after an ambiguous response or process restart.
Repeated agent/tool attempts must refer to that same logical operation; changing
the target is a new decision, not permission to repurpose the old key.

Begin with a transparent loop. LangGraph persistence or an MCP tool interface can
be a later comparison if useful; neither is required to demonstrate the idea.

## The most interesting recovery experiment

1. The user chooses a slot and the agent persists the reservation intent and key.
2. Alloca-Go commits the reservation, but a test proxy drops the response.
3. The agent process stops before recording the result.
4. A replacement process loads the pending operation and replays the same request
   with the same scoped key.
5. Check the authoritative rows and replayed result: one reservation hold and one
   logical capacity effect, with no duplicate schedule claim.

Compare this with a request that never committed. The recovered agent must handle
both possibilities without inventing a new key merely because it saw a timeout.
Record unresolved outcomes explicitly when recovery cannot yet determine them.

A replay returns the recorded operation outcome. It does not guarantee that an
earlier hold is still valid now: recovery must account for expiry before claiming
current availability or attempting a later confirmation. Persisting agent state
and committing an external service operation are separate transaction boundaries.

## Evidence to collect

Keep a small fixed task set: a normal reservation, an ambiguous date, a capacity
or schedule refusal, and the two recovery cases above.

Check final database state as well as the agent's explanation. Trace the intent,
model/tool calls, operation key, retries and recovered outcome. Record task
success, duplicate effects, response time and model cost; bound execution with
step and deadline limits.

The first useful result is a working reservation demonstration plus a recovery
trace. Confirmation/cancellation tools, multiple users, real authentication,
cloud deployment and deeper orchestration can follow only if the experiment
justifies them.

This is an idea to revisit at a suitable learning milestone, not a new active
curriculum or a commitment to change Alloca-Go's current roadmap.
