# Confirmatory verdicts — the six registered contrasts

> Generated snapshot — do not hand-edit. Produced by
> `eval/scripts/confirmatory-verdicts.py` at commit `1c024a5`;
> regenerate with `python3 eval/scripts/render-results-md.py`.

The pre-registered battery, executed exactly as registered at main
commit `96cc418` **before any confirmatory cell ran** ([registration](../ablation/preregistration.md)).
Eleven arms × 3 replications on the 4B, wave-ordered; McNemar exact
on the discordant issues per contrast, per wave and pooled;
Holm–Bonferroni across the six primaries at α=0.05.

Two effects shrank materially under replication (depth −9.2 → −5.2,
retrieval payload +6.4 → +3.0). That correction is the point of the
exercise and is left visible rather than restated.

```text
CONFIRMATORY VERDICTS — registration 96cc418, rig 4b
==============================================================================

contrast 1  A0 vs B  — hand-written rules (tool absent both sides)
   c1       74.4 vs  62.0   d=+12.4   disc  42/ 11   p=2.248e-05
   c2       75.2 vs  60.8   d=+14.4   disc  49/ 13   p=4.818e-06
   c3       73.6 vs  62.0   d=+11.6   disc  46/ 17   p=0.0003367
   POOLED   74.4 vs  61.6   d=+12.8   disc 137/ 41   p=2.747e-13   (n=750 paired)

contrast 2  G vs E2  — retrieval payload: full entries vs bare labels
   c1       73.2 vs  70.4   d= +2.8   disc  14/  7   p=0.1892
   c2       70.8 vs  70.4   d= +0.4   disc  11/ 10   p=1
   c3       74.0 vs  68.4   d= +5.6   disc  20/  6   p=0.009355
   c4       74.4 vs  71.2   d= +3.2   disc  16/  8   p=0.1516
   c5       73.6 vs  70.8   d= +2.8   disc  16/  9   p=0.2295
   POOLED   73.2 vs  70.2   d= +3.0   disc  77/ 40   p=0.0007996   (n=1250 paired)

contrast 3  P2 vs P1  — order: task before reference
   c1       77.2 vs  70.8   d= +6.4   disc  25/  9   p=0.009041
   c2       75.6 vs  69.6   d= +6.0   disc  23/  8   p=0.01067
   c3       76.4 vs  69.2   d= +7.2   disc  26/  8   p=0.002935
   POOLED   76.4 vs  69.9   d= +6.5   disc  74/ 25   p=8.506e-07   (n=750 paired)

contrast 4  T3 vs T2  — decomposition depth: 3 turns vs 2
   c1       70.8 vs  76.4   d= -5.6   disc  11/ 25   p=0.02882
   c2       71.6 vs  78.0   d= -6.4   disc   7/ 23   p=0.005223
   c3       72.0 vs  75.6   d= -3.6   disc   9/ 18   p=0.1221
   POOLED   71.5 vs  76.7   d= -5.2   disc  27/ 66   p=6.467e-05   (n=750 paired)

contrast 5a  S1 vs T2  — context bounding: fresh-session stage split
   c1       60.4 vs  76.4   d=-16.0   disc  16/ 56   p=2.397e-06
   c2       59.6 vs  78.0   d=-18.4   disc  15/ 61   p=9.843e-08
   c3       61.2 vs  75.6   d=-14.4   disc  12/ 48   p=3.184e-06
   POOLED   60.4 vs  76.7   d=-16.3   disc  43/165   p=4.811e-18   (n=750 paired)

contrast 5b  T2c vs T2  — context bounding: per-turn clear (distilled carrier)
   c1       66.8 vs  76.4   d= -9.6   disc  16/ 40   p=0.001842
   c2       62.8 vs  78.0   d=-15.2   disc  12/ 50   p=1.214e-06
   c3       65.6 vs  75.6   d=-10.0   disc  14/ 39   p=0.0008023
   POOLED   65.1 vs  76.7   d=-11.6   disc  42/129   p=1.741e-11   (n=750 paired)

contrast 6  T2cr vs T2c  — carrier vs clearing: raw notes vs distilled shortlist
   c1       71.6 vs  66.8   d= +4.8   disc  37/ 25   p=0.1619
   c2       72.0 vs  62.8   d= +9.2   disc  41/ 18   p=0.003794
   c3       72.4 vs  65.6   d= +6.8   disc  38/ 21   p=0.03634
   POOLED   72.0 vs  65.1   d= +6.9   disc 116/ 64   p=0.0001304   (n=750 paired)

==============================================================================
HOLM-BONFERRONI across the six registered primaries (alpha=0.05, pooled p)
   contrast 1  A0 vs B        raw p=2.747e-13  Holm-adj p=1.648e-12  RESOLVED
   contrast 2  G vs E2        raw p=0.0007996  Holm-adj p=0.0007996  RESOLVED
   contrast 3  P2 vs P1       raw p=8.506e-07  Holm-adj p=3.402e-06  RESOLVED
   contrast 4  T3 vs T2       raw p=6.467e-05  Holm-adj p=0.000194  RESOLVED
   contrast 5  S1/T2c vs T2   raw p=1.741e-11  Holm-adj p=8.704e-11  RESOLVED
   contrast 6  T2cr vs T2c    raw p=0.0001304  Holm-adj p=0.0002608  RESOLVED

------------------------------------------------------------------------------
BOUNDARY TRIGGER CHECK (signed: bump to 5x if a contrast lands within
1 point of its decision boundary; measured null band = +-2.4 points)
   contrast 1  A0    vs B     pooled d=+12.8  |d|-band=10.4  
   contrast 2  G     vs E2    pooled d= +3.0  |d|-band= 0.6  TRIGGER — 5x replication called for
   contrast 3  P2    vs P1    pooled d= +6.5  |d|-band= 4.1  
   contrast 4  T3    vs T2    pooled d= -5.2  |d|-band= 2.8  
   contrast 5a  S1    vs T2    pooled d=-16.3  |d|-band=13.9  
   contrast 5b  T2c   vs T2    pooled d=-11.6  |d|-band= 9.2  
   contrast 6  T2cr  vs T2c   pooled d= +6.9  |d|-band= 4.5  

NOTE: pooled p is the primary per this script's stated judgment call;
per-wave p is printed so the alternative combination is auditable.
```
