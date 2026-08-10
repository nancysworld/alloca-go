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
