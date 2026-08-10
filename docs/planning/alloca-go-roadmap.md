# Alloca-Go — exploration roadmap

**Status:** Living — directional, non-normative, unscheduled.

**Purpose:** map the areas, questions, capabilities, and hypotheses that may be worth exploring
through Alloca-Go, without committing them to a particular milestone, implementation,
architecture, or delivery date.

It answers one question:

> **Where might this project go, and what is worth learning or proving?**

## What this document is not

It does **not** own current implementation state, established facts or evidence, normative
requirements or invariants, accepted architecture, validation obligations, milestone or PR scope,
dates, budget, or implementation status. Every one of those has a better owner, listed in
[`../README.md`](../README.md).

Inclusion here means **"potentially valuable direction"**, never "committed scope". An item may
sit here for a long time, be reframed by evidence, or be dropped without ceremony.

## Where it sits in the engineering process

The roadmap is **outside the engineering iteration loop and upstream of Goal selection**:

```text
             EXPLORATION ROADMAP
        possible areas / questions / directions
                         |
              select a worthwhile outcome
                         v
                        GOAL
                         |
                         v
Problem -> Requirements -> Design -> Validation plan -> Schedule -> Implement
   ^                                                                |
   |                                                                v
   +------ next Problem <- Analyse & Review <- Evidence -------------+
```

It is **not another mandatory process stage**. A roadmap item becomes committed work only when the
maintainer deliberately selects it into a Goal or Problem, and it then proceeds through
Requirements, Design, Validation plan, and Schedule like any other work
([`../development/engineering-process.md`](../development/engineering-process.md) §1.4).

The arrow runs the other way too. Analyse & Review may surface an interesting question that is not
worth pursuing now; adding or reframing it here is the correct home for it, rather than
prematurely turning it into a requirement or a scheduled PR.

## The connected questions

The areas below elaborate four questions the project exists to explore:

1. How should scarce or conserved state be owned and mutated correctly under concurrency?
2. What is the sustainable, SLO-compliant throughput of one capacity unit?
3. How far can those units scale before a shared dependency or hot authority becomes limiting?
4. How should the system degrade under overload, so users receive explicit bounded outcomes
   instead of latency growth, timeouts, and generic errors?

Workload scenarios referenced below — a synchronized booking release across many independent
organisations, and contention for a shared inventory quantity or conserved balance — are
**synthetic engineering models**. They are not descriptions of any organisation's architecture,
scale, traffic, or implementation.

---

## Transactional correctness and write authority

**Why it is worth exploring:** every other question in this project depends on this one being
settled. A scaling result measured on a system that admits double-booking measures nothing.

**Questions worth answering:**

- Which state genuinely needs a single write authority, and which only appears to?
- What is the cheapest mechanism that preserves an invariant under real concurrency?
- Where does correctness require serialization that no amount of compute removes?
- How much correctness can be pushed into the database's own guarantees rather than application
  protocol?

**Related durable owners:** [`../design/transaction-semantics.md`](../design/transaction-semantics.md)
(`INV-*`), [`../decisions/0002-postgresql-transactional-authority.md`](../decisions/0002-postgresql-transactional-authority.md).

## Writable-state horizontal scaling

**Why it is worth exploring:** adding stateless compute is well understood; partitioning writable
transactional state without weakening correctness is where the interesting failures live.

**Questions worth answering:**

- When independent work is placed on independent writers, what actually composes and what does not?
- What does a booking spanning two writable domains cost, and is that cost ever worth paying?
- How should placement be owned, versioned, and enforced so a routing mistake is detected rather
  than silently written?
- What does rebalancing or online placement change require of the correctness model?

**Related durable owners:** [`../design/horizontal-scaling.md`](../design/horizontal-scaling.md),
[`../design/horizontal-database-authority.md`](../design/horizontal-database-authority.md),
REQ-SCALE-1, REQ-ROUTE-1.

## Stateless service scaling

**Why it is worth exploring:** the cheap axis, and therefore the one most likely to be
over-credited with gains that came from somewhere else.

**Questions worth answering:**

- How much useful capacity does another replica add once the shared writer is the constraint?
- How is added compute distinguished from changed pressure on the database admission boundary?
- What does replica identity need to expose for a multi-replica result to be interpretable?

**Related durable owners:** `horizontal-scaling.md`, REQ-SCALE-2, REQ-SCALE-3.

## Overload, admission, and fairness

**Why it is worth exploring:** this is the founding unreproduced question. A predecessor prototype
showed latency growing until timeouts became the visible failure mode, and this project has not yet
reproduced that mechanism under controlled conditions
([`../design/high-level-design.md`](../design/high-level-design.md) §1.1).

**Questions worth answering:**

- Where does overload first accumulate — queue, pool, lock, or authority?
- Can explicit bounded admission produce better user-visible behaviour than letting contention
  accumulate into timeouts?
- Which admission mechanisms preserve useful goodput rather than merely relocating the wait?
- What fairness properties matter when many users contend for one scarce resource?
- When is retry preferable to queuing, and what does retry guidance need to carry to be safe?
- Does an open-loop arrival model expose behaviour a closed-loop harness structurally cannot?

**Related durable owners:** [`../design/measurement-contract.md`](../design/measurement-contract.md)
(outcome taxonomy, overload objective),
[`../design/latency-timeouts-and-retries.md`](../design/latency-timeouts-and-retries.md).

## Failure, ambiguity, and recovery

**Why it is worth exploring:** a distributed system's honesty is tested by what it does when it
cannot know whether a mutation committed.

**Questions worth answering:**

- Which faults produce genuinely ambiguous outcomes, and which only look ambiguous?
- What is the smallest protocol that makes an ambiguous mutation safely resolvable?
- How is failure contained to the dependency that failed rather than spreading through routing?
- What recovery requires no compensating writes anywhere else, and what does not?

**Related durable owners:** `transaction-semantics.md` (INV-21), REQ-COR-2, REQ-FAIL-1,
[`../test/validation-plan/ag-sept-validation-plan.md`](../test/validation-plan/ag-sept-validation-plan.md).

## Distributed coordination beyond a single writable domain

**Why it is worth exploring:** deliberately deferred rather than solved, and the deferral is only
honest while the cost of lifting it is understood.

**Questions worth answering:**

- What correctness model would a cross-domain booking actually need?
- Where would durable coordination state live, and what owns it?
- What would recovery and replay mean across domains that fail independently?
- Is the operational cost of coordination ever lower than the cost of avoiding it by placement?

**Related durable owners:** `horizontal-database-authority.md` §4.2.

## Operational deployment and observability

**Why it is worth exploring:** an architecture that cannot be deployed, observed, or identified is
not evidence of anything.

**Questions worth answering:**

- What must a deployment expose for a measured result to be reproducible a year later?
- Which observability is diagnostic, and which is merely reassuring?
- What does a managed or cloud environment change about timeout chains, network boundaries, and
  failure modes that a workstation cannot show?
- What is the smallest orchestration that preserves the architectural properties that matter?

**Related durable owners:** [`../design/deployment-architecture.md`](../design/deployment-architecture.md),
[`../design/observability.md`](../design/observability.md),
[`../decisions/0003-deployed-artifact-identity.md`](../decisions/0003-deployed-artifact-identity.md).

## Capacity economics

**Why it is worth exploring:** throughput per unit cost is the question an operator actually asks,
and it is not the same question as peak benchmark throughput.

**Questions worth answering:**

- What is sustainable, SLO-compliant, resilient throughput per unit of cost?
- What does a recommended operating capacity reserve headroom *for*, and how is each component
  measured rather than assumed?
- How much does correctness machinery cost, and is that cost worth naming separately?
- Which capacity unit preserves failure isolation, deployment safety, and N+1 headroom, rather
  than maximising a single number?

**Related durable owners:** `measurement-contract.md` §3, §3.1, §3.2.

## Additional conserved-resource scenarios

**Why it is worth exploring:** the booking domain is one shape of scarce-resource contention.
Others stress the model differently.

**Questions worth answering:**

- Does a divisible quantity or conserved balance behave like slot capacity, or expose a different
  serialization frontier?
- What changes when a reservation holds several units rather than one?
- Does a synchronized release wave — many users converging on a small set of resources at a known
  instant — produce behaviour the isolated controls do not?
- Which of these scenarios is worth the fixture and harness cost, and which is a variation that
  teaches nothing new?

**Related durable owners:** validation plan §3 (controlled workloads).

## Service and deployment boundaries, and asynchronous extensions

**Why it is worth exploring:** the project starts as a modular monolith deliberately, and the
interesting question is what evidence would justify changing that.

**Questions worth answering:**

- What boundary, if any, does the evidence justify extracting — and what would it buy?
- Would an asynchronous event or reporting consumer fed by a transactional outbox demonstrate a
  real consistency and failure boundary, or only add moving parts?
- Which boundaries are genuinely independent failure and scaling domains, and which are
  process-local optimisations wearing architectural language?

**Related durable owners:** [`../decisions/0001-modular-monolith-first.md`](../decisions/0001-modular-monolith-first.md),
`horizontal-scaling.md` §12.

---

## Selecting from here

Nothing above is scheduled. When one of these becomes worth doing, it is framed as a Goal or
Problem under [`../requirements/`](../requirements/), and the milestone plan schedules the work.
The current milestone's goal and open problem are in
[`../requirements/ag-sept.md`](../requirements/ag-sept.md); what is actually being built now is in
[`ag-sept-plan.md`](ag-sept-plan.md).
