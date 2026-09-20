# Reviewer benchmarks

Scripts that answer specific questions raised in review. Run from the repo root,
they import `benchmark_simulation.py` and reuse its scenarios and seed.

```bash
python3 benchmarks/bench_variants.py
python3 benchmarks/bench_strand_penalty.py
```

## bench_variants.py

Answers two points from koordinator-sh/koordinator#2839 (2026-07-21).

1. Epsilon gated alignment vs the unconditional 0.2 term. Alignment is applied
   only when variance is near tied between candidate nodes, which is the true
   tiebreaker reading of the design prose. Tested at eps 0.5, 1, 2, 5, 10.

2. args on BalancedAllocation vs a separate plugin. Variance plus strand plus
   pressure, with no separate alignment term.

Result: epsilon gating loses at every eps tested. Stranded goes from 34 back to
44 to 49, level with plain BalancedAllocation. The args only variant recovers
the balance score but not the stranding. The alignment term is what produces
the stranding reduction.

Note that a true tiebreaker cannot be expressed as a Score plugin, since the
scorer signature is per node and the comparison is between nodes. It requires
two pass selection.

## bench_strand_penalty.py

Answers the continuous strand penalty question from
koordinator-sh/koordinator#2837, raised 2026-04-04 and again 2026-05-14.

Measures top 5 candidate score spread and exact ties, threshold 80/20 against a
continuous penalty proportional to pair spread.

Result: spread widens about 20 percent on average, most in GPU heavy scenarios,
slightly narrower in CPU only. Ties are unchanged, 137 against 135. So the
continuous form does spread scores but does not break ties, which was the
stated criterion.
