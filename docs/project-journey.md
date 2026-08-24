# Alloca-Go — Project Journey

I did not start Alloca-Go with a final architecture in mind.

It grew out of an earlier booking prototype and one question that I had not answered properly: as concurrency increased, response time got worse until timeouts appeared — but why?

I wanted a system where I could investigate questions like that without compromising correctness just to make the benchmark easier. A booking service turned out to be a useful playground: under contention, capacity, retries, timeouts, idempotency and overlapping reservations all start interacting with each other.

So I started again in Go.

## Correctness first

The first phase was not about scale. It was about making the state trustworthy.

Who owns slot capacity? How do I stop the same user booking overlapping sessions? What happens when a request times out after the database may already have committed it? How should retries behave?

PostgreSQL became the authority for those decisions, and I kept the application as a modular monolith. I did not want to split it into services simply because distributed systems were one of the things I wanted to explore.

The rule was: find a real boundary first, then decide whether it deserves to be distributed.

That decision became important later.

## Finding the first limit

Once the transactional core was solid enough, I could ask a much more interesting question:

> What actually limits this system first?

I did not know whether the answer would be Go, the database, the connection pool, telemetry, the load generator or something else.

The first sustained experiment reached roughly 4,300 booking requests per second on my workstation. Beyond that point, adding concurrency mostly increased latency.

What surprised me was how little CPU the Go service needed.

The first real limit was PostgreSQL.

I could see some clues about what was happening inside PostgreSQL, but not enough to claim that I understood the exact mechanism. That distinction became important to the project: knowing which subsystem is limiting is not the same as knowing precisely why.

More importantly, the result changed what I wanted to do next.

Adding more Go service instances against the same database suddenly looked much less interesting. If the writer was already the constraint, I wanted to know whether the writable authority itself could be divided.

## From one database to several

That led to the next phase.

Independent organisations seemed like natural candidates for independent database authorities. But there was an immediate complication: a booking involves both the slot and the user's schedule.

If those belong to different databases, one PostgreSQL transaction can no longer protect the whole operation.

I decided not to solve that problem yet.

Instead, the first distributed version only supported bookings that stayed inside one database authority. Cross-authority bookings were explicitly refused.

I preferred a clearly unsupported case to implementing two independent database commits and pretending they had the same semantics as one transaction.

This gave Alloca the idea of a **shard group**: a writable PostgreSQL authority with the service instances that use it.

The next experiments showed that two such authorities could operate independently, preserve the existing correctness rules, and isolate failures from each other.

That was a useful distributed-systems result.

But it still was not a scaling result, because both databases were running on the same machine.

## Trying to measure scaling

The obvious next question was whether shard groups were not only correctness boundaries, but also useful **capacity units**.

I built an experiment comparing one, two and four groups.

This is where things became more interesting than expected.

The four-group measurement was reasonably stable. The one- and two-group measurements were not. Identical runs could produce surprisingly different throughput.

Without a trustworthy G1 baseline, calculating a scaling efficiency would have been misleading, so I did not calculate one.

The extra observations pointed towards something in the shared write path rather than the Go service itself. But again, they did not tell me exactly what mechanism underneath the storage stack was responsible.

Eventually I realised that I was no longer just measuring Alloca.

I was measuring Alloca **plus my workstation**.

CPU partitioning could isolate some resources, but the databases still shared the machine, Docker Desktop, the kernel, memory hierarchy and storage path.

The test environment itself had become part of the question.

## Knowing when to stop

The next plan was to repeat the experiment using independently provisioned cloud instances.

I prepared the experiment, but the AWS quota available at the time was not enough to provision the complete topology.

So I stopped there.

There is no hidden AWS result and no estimated scaling number. The experiment simply remains unfinished.

I now think there is also a better step before returning to the multi-instance scaling test.

First, I want to establish a reliable **single-authority capacity frontier** on an independent cloud instance, with the load generator elsewhere. I want to understand what limits that single unit and why the previous results varied as much as they did.

Only when G1 is something I trust does it make sense to use it as the denominator for G2 and G4.

So the sequence has become:

> **Understand one capacity unit first. Then ask how several of them compose.**

That feels much stronger than treating G1 as merely the first row in a benchmark table.

## A new phase

This is also where the direction of Alloca is changing.

So far, the project has been driven by engineering experience, measurements, hypotheses and what each experiment revealed. That has taken it a long way.

Recently I started reading Brendan Gregg's *Systems Performance*, and it immediately felt familiar.

For many years I have worked on performance problems using experience, instinct, measurements and experiments. Some of the techniques in the book may turn out to be things I have already been doing in another form. Others may give me better ways to reason about problems I have struggled to isolate.

That makes the next phase particularly interesting to me.

I want to slow the building down for a while, study established systems-performance methodology, compare it with how I have approached Alloca so far, and see what I can learn from it.

Then I can apply the useful parts selectively.

The next progress in Alloca may therefore not be another feature or even another big benchmark. It might be understanding a methodology, looking at an old result differently, adding one missing measurement, or designing a smaller experiment that tells me which of two explanations is more likely.

The principle I want to carry forward is:

> **Methodology informs the investigation. Evidence decides the conclusion. Engineering judgement chooses the next useful question.**

I still do not know exactly where Alloca will end up.

It may lead deeper into PostgreSQL, Linux performance, workload modelling, scaling, failure behaviour, distributed coordination — or something I have not thought of yet.

But I now have a clearer idea of what the project is for.

> **Alloca is a real stateful system I can use to learn how systems behave — combining experience, established methodology and experiments, and letting the evidence decide what is worth exploring next.**
