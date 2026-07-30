# Confirmatory verdicts — the six registered contrasts

> Generated snapshot — do not hand-edit. Produced by
> `eval/scripts/confirmatory-verdicts.py` at commit `9e7feeb`;
> regenerate with `python3 eval/scripts/render-results-md.py`.

The pre-registered battery, executed exactly as registered at main
commit `96cc418` **before any confirmatory cell ran** ([registration](../ablation/preregistration.md)).
Eleven arms × 3 replications on the 4B, wave-ordered; McNemar exact
on the discordant issues per contrast, per wave and pooled;
Holm–Bonferroni across the six primaries at α=0.05.

**Five of six resolve.** The primary estimator is an issue-clustered
permutation test (Amendment 2): repeats of the same 250 issues are not
independent trials, so the pooled McNemar shown below for continuity
overstates significance and is *not* the verdict.

Three effects moved under scrutiny, all against the author's interest:
depth −9.2 → −5.2 and retrieval payload +6.4 → +3.0 under replication,
and retrieval payload from RESOLVED to **UNRESOLVED** under the
clustered estimator. Those corrections are the point of the exercise
and are left visible rather than restated.

```text
CONFIRMATORY VERDICTS — registration 96cc418, rig 4b
==============================================================================

contrast 1  A0 vs B  — hand-written rules (tool absent both sides)
   c1       74.4 vs  62.0   d=+12.4   disc  42/ 11   p=2.248e-05
   c2       75.2 vs  60.8   d=+14.4   disc  49/ 13   p=4.818e-06
   c3       73.6 vs  62.0   d=+11.6   disc  46/ 17   p=0.0003367
   pooled   74.4 vs  61.6   d=+12.8   disc 137/ 41   p=2.747e-13   (descriptive only - assumes independence it lacks)
   CLUSTER d=+12.8   82/250 issues informative   p=5e-05   <- PRIMARY (issue-clustered permutation)

contrast 2  G vs E2  — retrieval payload: full entries vs bare labels
   c1       73.2 vs  70.4   d= +2.8   disc  14/  7   p=0.1892
   c2       70.8 vs  70.4   d= +0.4   disc  11/ 10   p=1
   c3       74.0 vs  68.4   d= +5.6   disc  20/  6   p=0.009355
   c4       74.4 vs  71.2   d= +3.2   disc  16/  8   p=0.1516
   c5       73.6 vs  70.8   d= +2.8   disc  16/  9   p=0.2295
   pooled   73.2 vs  70.2   d= +3.0   disc  77/ 40   p=0.0007996   (descriptive only - assumes independence it lacks)
   CLUSTER d= +3.0   44/250 issues informative   p=0.0667   <- PRIMARY (issue-clustered permutation)

contrast 3  P2 vs P1  — order: task before reference
   c1       77.2 vs  70.8   d= +6.4   disc  25/  9   p=0.009041
   c2       75.6 vs  69.6   d= +6.0   disc  23/  8   p=0.01067
   c3       76.4 vs  69.2   d= +7.2   disc  26/  8   p=0.002935
   pooled   76.4 vs  69.9   d= +6.5   disc  74/ 25   p=8.506e-07   (descriptive only - assumes independence it lacks)
   CLUSTER d= +6.5   46/250 issues informative   p=0.00155   <- PRIMARY (issue-clustered permutation)

contrast 4  T3 vs T2  — decomposition depth: 3 turns vs 2
   c1       70.8 vs  76.4   d= -5.6   disc  11/ 25   p=0.02882
   c2       71.6 vs  78.0   d= -6.4   disc   7/ 23   p=0.005223
   c3       72.0 vs  75.6   d= -3.6   disc   9/ 18   p=0.1221
   pooled   71.5 vs  76.7   d= -5.2   disc  27/ 66   p=6.467e-05   (descriptive only - assumes independence it lacks)
   CLUSTER d= -5.2   52/250 issues informative   p=0.0036   <- PRIMARY (issue-clustered permutation)

contrast 5a  S1 vs T2  — context bounding: fresh-session stage split
   c1       60.4 vs  76.4   d=-16.0   disc  16/ 56   p=2.397e-06
   c2       59.6 vs  78.0   d=-18.4   disc  15/ 61   p=9.843e-08
   c3       61.2 vs  75.6   d=-14.4   disc  12/ 48   p=3.184e-06
   c6       62.8 vs  75.2   d=-12.4   disc  16/ 47   p=0.0001171
   c7       60.8 vs  75.2   d=-14.4   disc  15/ 51   p=1.01e-05
   c8       61.6 vs  75.2   d=-13.6   disc  15/ 49   p=2.436e-05
   pooled   61.1 vs  75.9   d=-14.9   disc  89/312   p=3.995e-30   (descriptive only - assumes independence it lacks)
   CLUSTER d=-14.9   125/250 issues informative   p=5e-05   <- PRIMARY (issue-clustered permutation)

contrast 5b  T2c vs T2  — context bounding: per-turn clear (distilled carrier)
   c1       66.8 vs  76.4   d= -9.6   disc  16/ 40   p=0.001842
   c2       62.8 vs  78.0   d=-15.2   disc  12/ 50   p=1.214e-06
   c3       65.6 vs  75.6   d=-10.0   disc  14/ 39   p=0.0008023
   pooled   65.1 vs  76.7   d=-11.6   disc  42/129   p=1.741e-11   (descriptive only - assumes independence it lacks)
   CLUSTER d=-11.6   94/250 issues informative   p=5e-05   <- PRIMARY (issue-clustered permutation)

contrast 6  T2cr vs T2c  — carrier vs clearing: raw notes vs distilled shortlist
   c1       71.6 vs  66.8   d= +4.8   disc  37/ 25   p=0.1619
   c2       72.0 vs  62.8   d= +9.2   disc  41/ 18   p=0.003794
   c3       72.4 vs  65.6   d= +6.8   disc  38/ 21   p=0.03634
   pooled   72.0 vs  65.1   d= +6.9   disc 116/ 64   p=0.0001304   (descriptive only - assumes independence it lacks)
   CLUSTER d= +6.9   100/250 issues informative   p=0.0031   <- PRIMARY (issue-clustered permutation)

==============================================================================
HOLM-BONFERRONI across the six registered primaries (alpha=0.05, clustered p)
   contrast 1  A0 vs B        raw p=5e-05  Holm-adj p=0.0003  RESOLVED
   contrast 2  G vs E2        raw p=0.0667  Holm-adj p=0.0667  unresolved
   contrast 3  P2 vs P1       raw p=0.00155  Holm-adj p=0.0062  RESOLVED
   contrast 4  T3 vs T2       raw p=0.0036  Holm-adj p=0.0093  RESOLVED
   contrast 5  S1/T2c vs T2   raw p=5e-05  Holm-adj p=0.0003  RESOLVED
   contrast 6  T2cr vs T2c    raw p=0.0031  Holm-adj p=0.0093  RESOLVED

------------------------------------------------------------------------------
BOUNDARY TRIGGER CHECK (signed: bump to 5x if a contrast lands within
1 point of its decision boundary; measured null band = +-2.4 points)
   contrast 1  A0    vs B     pooled d=+12.8  |d|-band=10.4  
   contrast 2  G     vs E2    pooled d= +3.0  |d|-band= 0.6  TRIGGER — 5x replication called for
   contrast 3  P2    vs P1    pooled d= +6.5  |d|-band= 4.1  
   contrast 4  T3    vs T2    pooled d= -5.2  |d|-band= 2.8  
   contrast 5a  S1    vs T2    pooled d=-14.9  |d|-band=12.5  
   contrast 5b  T2c   vs T2    pooled d=-11.6  |d|-band= 9.2  
   contrast 6  T2cr  vs T2c   pooled d= +6.9  |d|-band= 4.5  

PRIMARY = issue-clustered permutation (Amendment 2). Pooled McNemar is
printed for continuity but is NOT the verdict: it treats repeats of one
issue as independent trials and so overstates significance.
```
