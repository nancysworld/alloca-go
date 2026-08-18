# Alloca-Go documentation

Start with [`design/high-level-design.md`](design/high-level-design.md) for the system problem,
architecture, and detailed design ownership map.

For **how engineering work proceeds**, start with
[`development/engineering-process.md`](development/engineering-process.md). It owns the model: a
durable **Goal** sits above repeated engineering iterations.

```text
                               GOAL
             What worthwhile outcome are we trying to achieve,
                    and what would make us stop?
                                |
                                v
Problem -> Requirements -> Design -> Validation plan -> Schedule -> Implement
   ^                                                                |
   |                                                                v
   +------ next problem <- Analyse & Review <- Evidence ------------+
                            |             |
                            |             +-- goal not yet sufficiently achieved
                            |
                            +-- goal sufficiently achieved -> END
```

This directory separates documents by the kind of truth they own so that evidence can change
the next iteration without making durable code or architecture depend on a mutable milestone
plan.

| Area | Primary question |
|---|---|
| [`requirements/`](requirements/) | What worthwhile outcome are we trying to achieve, what durable problem currently blocks it, and what must be true? |
| [`design/`](design/) | What durable system shape and contracts satisfy those requirements? |
| [`test/validation-plan/`](test/validation-plan/) | How will we prove or falsify the requirements and design claims? |
| [`planning/`](planning/) | What have we chosen to do now, when, with what priority, budget, and descope order? The **exploration roadmap** lives here too, answering the different question of where the project *might* go — directional, unscheduled, and upstream of Goal selection. |
| [`decisions/`](decisions/) | Why was a consequential architectural choice made? |
| [`development/`](development/) | How do we work, and what did implementation discover or actually ship? |
| [`operations/`](operations/) | How do we build, run, deploy, and operate the system? |
| [`measurements/`](measurements/) | What did experiments establish, and where is the retained evidence? |

The lifecycle and ownership rules above are normative in `development/engineering-process.md`;
this README is the navigation entry point, not a second definition of the process.

A fact should have one normative home. Other documents link to that owner rather than maintaining
a competing copy.

## Diagram convention

Durable documentation should use diagrams when they materially improve **first-read
comprehension** of structure, topology, flow, ownership, sequence, state transitions, or comparison.
The purpose is to give a reader the mental model quickly; precise prose still owns the exact
semantics.

Prefer small, reviewable diagrams close to the text they explain. **Mermaid is the default** when it
can express the idea clearly because its source remains version-controlled beside the document.
ASCII diagrams remain appropriate when they are simpler or more portable.

A useful diagram should have one clear job and should complement rather than duplicate a large
block of prose. Do not add diagrams only for decoration. If a diagram carries normative semantics,
the owning document and surrounding text must make that explicit; otherwise it is explanatory.
When the adjacent contract changes, update the diagram in the same change so an intuitive picture
cannot silently become stale.
