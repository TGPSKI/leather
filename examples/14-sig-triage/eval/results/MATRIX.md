# Results matrix — every archived cell

> Generated snapshot — do not hand-edit. Produced by
> `eval/scripts/table.py` at commit `6c484d0`;
> regenerate with `python3 eval/scripts/render-results-md.py`.

Accuracy per archived cell (accept-set, abstention-aware — the
`sigeval` scorer of record), with the variable each arm isolates.
Cells are read against their declared comparison arm, never against
the leaderboard: see [VERDICTS.md](VERDICTS.md) for the paired
inference and [README.md](README.md) for how to read any number
here (means with spread, the ±6-row null band, the failing gate).

```text

  35b  34 cells
     cell           acc no-out calls/iss  tools   ktok  variable under test                   
                                                ? = not  captured (archive predates usage log) 
     35b-A-4       89.2      -      2.00      0    644  baseline — the committed pipeline     
     35b-H-4       88.4      -      2.00    250   1004  does the catalog ADD to the rules     
     35b-A-5       88.0      -      2.00      0    644  baseline — the committed pipeline     
     35b-F2        88.0      6      2.00    250   1077  the stage split, rules AND catalog han
     35b-A-2       87.6      -      1.00      0      ?  baseline — the committed pipeline     
     35b-A-3       87.6      -      2.00      0    645  baseline — the committed pipeline     
     35b-A-6       87.6      -      2.00      0    644  baseline — the committed pipeline     
     35b-H-5       87.6      -      2.00    250   1002  does the catalog ADD to the rules     
     35b-H         87.2      -      2.00    250      ?  does the catalog ADD to the rules     
     35b-A-7       86.8      -      2.00      0    643  baseline — the committed pipeline     
     35b-A         86.8      -      2.00      0      ?  baseline — the committed pipeline     
     35b-H-3       86.8      -      2.00    250   1003  does the catalog ADD to the rules     
     35b-H-2       86.4      -      2.00    250   1003  does the catalog ADD to the rules     
     35b-H-6       86.4      -      2.00    250   1003  does the catalog ADD to the rules     
     35b-A0        85.6      -      1.00      0      ?  cost of an offered-but-unused tool    
     35b-F         85.2      -      1.00      0    853  the analyze->match STAGE SPLIT        
     35b-T2        85.2      -      3.08    270   1686  turn decomposition (leather-native, no
     35b-H2        83.6      1      2.00    250    902  rules + NARROWED retrieval (vs rules +
     35b-D         82.4      1      2.00    250      ?  tool round + second forward pass, orde
     35b-G2        80.8      1      2.00    250    724  boundaries enforced in code vs offered
     35b-G         79.6      1      2.00    250    742  retrieval payload: entries vs labels  
     35b-T2c-2     79.6      4      4.66    667   2640  per-turn context clearing (bounded dec
     35b-Dn        79.2      1      2.00    250      ?  instruction WORDING, mechanism held   
     35b-E2        78.8      -      2.00    250      ?  hand-written NOT_MATCH boundaries     
     35b-P1        78.4      -      1.00      0    592  message ROLE (system vs user), order h
     35b-P2        78.4      -      1.00      0    594  ORDER (before vs after the issue), rol
     35b-S1        78.0      1      2.97    243    592  bounded context via a STAGE boundary  
     35b-T3        78.0      -      4.03    258   2099  decomposition DEPTH (3 turns vs 2)    
     35b-E         77.6      -      2.00    250      ?  retrieval payload: narrowed labels vs 
     35b-C2        75.6      -      1.00      0      ?  catalog FORMAT (prose vs term index), 
     35b-C         74.8      -      1.00      0      ?  catalog content, delivered in the syst
     35b-T2c       74.8      3      4.82    707   2785  per-turn context clearing (bounded dec
     35b-Eauto     70.4      -      1.00      1      ?  voluntary vs compelled tool use       
     35b-B         68.4      -      1.00      0      ?  floor — model prior only              

  4b  22 cells
     cell           acc no-out calls/iss  tools   ktok  variable under test                   
                                                ? = not  captured (archive predates usage log) 
     4b-F2         81.6      2      1.99    248   1165  the stage split, rules AND catalog han
     4b-F          80.4      -      1.59    148   1427  the analyze->match STAGE SPLIT        
     4b-H          78.8      1      2.00    460   1098  does the catalog ADD to the rules     
     4b-H2         78.0      4      1.98    272    805  rules + NARROWED retrieval (vs rules +
     4b-A          77.6      1      3.00    250      ?  baseline — the committed pipeline     
     4b-P2         77.6      1      1.00      0    575  ORDER (before vs after the issue), rol
     4b-T2         77.2      4      3.04    254   1497  turn decomposition (leather-native, no
     4b-G          75.6      2      1.99    258    645  retrieval payload: entries vs labels  
     4b-A0         75.2      1      1.03      0    304  cost of an offered-but-unused tool    
     4b-G2         74.8      6      1.96    273    628  boundaries enforced in code vs offered
     4b-Dn         72.4      1      2.00    499    968  instruction WORDING, mechanism held   
     4b-D          71.6      5      2.03    512    988  tool round + second forward pass, orde
     4b-E          71.6      4      1.98    260      ?  retrieval payload: narrowed labels vs 
     4b-P1         71.6      1      1.00      0    575  message ROLE (system vs user), order h
     4b-Eauto      70.0      2      2.00    250      ?  voluntary vs compelled tool use       
     4b-E2         69.2      3      1.98    267      ?  hand-written NOT_MATCH boundaries     
     4b-T3         68.0      1      4.04    253   1925  decomposition DEPTH (3 turns vs 2)    
     4b-C          67.2      1      1.00      0      ?  catalog content, delivered in the syst
     4b-C2         66.8      1      1.00      0      ?  catalog FORMAT (prose vs term index), 
     4b-T2c        66.0      3      4.09    516   1953  per-turn context clearing (bounded dec
     4b-B          62.8      1      1.00      0      ?  floor — model prior only              
     4b-S1         62.4     25      3.00    251    516  bounded context via a STAGE boundary  
```
