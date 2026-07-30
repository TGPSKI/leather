# Paired verdicts — every declared comparison

> Generated snapshot — do not hand-edit. Produced by
> `eval/scripts/paired-verdicts.py` at commit `9e7feeb`;
> regenerate with `python3 eval/scripts/render-results-md.py`.

McNemar's exact test on the discordant issues for each declared
arm pair, from archives and manifests (never runner logs).
RESOLVED means p < 0.05 on the paired flips; anything inside the
±6-row null band is reported unresolved — *the experiment could
not tell*, not "no change". Confounds are flagged from manifest
diffs, not narrated away.

```text

== 35b ==
35b-A-2     vs 35b-A        87.6 vs  86.8  d= +0.8  disc  5/ 3  p=0.7266  NOISE      REPEAT — config held; discordance here IS the draw noise
    !! analyze_cache_sha: 'e9afa2fbe398' vs 'none'
       analyze_cache: 'eval/results/runs/35b-A/analyze-notes.jsonl' vs 'none'
    ~  git_commit: 'a1b3f71' vs 'f94aac3'
35b-H-2     vs 35b-H        86.4 vs  87.2  d= -0.8  disc  1/ 3  p=0.6250  NOISE      REPEAT — config held; discordance here IS the draw noise
    ~  git_commit: '31a39bf' vs 'a1b3f71'
35b-T2c-2   vs 35b-T2c      79.6 vs  74.8  d= +4.8  disc 25/13  p=0.0730  PROMPT-DIFF REPEAT — config held; discordance here IS the draw noise
35b-A0      vs 35b-A        85.6 vs  86.8  d= -1.2  disc  4/ 7  p=0.5488  unresolved cost of an offered-but-unused tool
    ≈  analyze_cache_sha: 'e9afa2fbe398' vs 'none'
    ≈  analyze_cache: 'eval/results/runs/35b-A/analyze-notes.jsonl' vs 'none'
    ~  git_commit: 'a1b3f71' vs 'f94aac3'
35b-C       vs 35b-B        74.8 vs  68.4  d= +6.4  disc 28/12  p=0.0166  RESOLVED   catalog content, delivered in the system prompt
35b-C2      vs 35b-C        75.6 vs  74.8  d= +0.8  disc 14/12  p=0.8450  unresolved catalog FORMAT (prose vs term index), delivery held
35b-D       vs 35b-P2       82.4 vs  78.4  d= +4.0  disc 23/13  p=0.1325  unresolved tool round + second forward pass, order held
    ≈  analyze_cache_sha: 'e9afa2fbe398' vs 'c9a77f0ab3a5'
       force_tool: '1' vs '0'
    ≈  analyze_cache: 'eval/results/runs/35b-A/analyze-notes.jsonl' vs 'eval/.caches/analyze-cache-P2-35b.jsonl'
    ~  git_commit: 'a1b3f71' vs '1d9fce5'
35b-Dn      vs 35b-D        79.2 vs  82.4  d= -3.2  disc  5/13  p=0.0963  unresolved instruction WORDING, mechanism held
35b-E       vs 35b-D        77.6 vs  82.4  d= -4.8  disc 16/28  p=0.0961  unresolved retrieval payload: narrowed labels vs whole catalog
35b-E2      vs 35b-E        78.8 vs  77.6  d= +1.2  disc 11/ 8  p=0.6476  unresolved hand-written NOT_MATCH boundaries
       index: 'sigs.index.seeded.tsv' vs 'sigs.index.tsv'
       index_sha: 'f5f249b60d4d' vs '2cbb419f6695'
35b-Eauto   vs 35b-E        70.4 vs  77.6  d= -7.2  disc 14/32  p=0.0114  RESOLVED   voluntary vs compelled tool use
       force_tool: '0' vs '1'
35b-F       vs 35b-A        85.2 vs  86.8  d= -1.6  disc 10/14  p=0.5413  unresolved the analyze->match STAGE SPLIT
    ~  git_commit: '1d9fce5' vs 'f94aac3'
35b-F2      vs 35b-H2       88.0 vs  83.6  d= +4.4  disc 27/16  p=0.1263  unresolved the stage split, rules AND catalog handling held constant
    ≈  analyze_cache_sha: 'none' vs 'e9afa2fbe398'
    ≈  analyze_cache: 'none' vs 'eval/results/runs/35b-A/analyze-notes.jsonl'
    ~  git_commit: '698cd91' vs '31a39bf'
35b-G       vs 35b-E2       79.6 vs  78.8  d= +0.8  disc 11/ 9  p=0.8238  unresolved retrieval payload: entries vs labels
    ~  git_commit: '1d9fce5' vs 'a1b3f71'
35b-G2      vs 35b-G        80.8 vs  79.6  d= +1.2  disc 11/ 8  p=0.6476  unresolved boundaries enforced in code vs offered as advice
    ~  git_commit: '0f36f00' vs '1d9fce5'
35b-H       vs 35b-A        87.2 vs  86.8  d= +0.4  disc  7/ 6  p=1.0000  unresolved does the catalog ADD to the rules
    ≈  analyze_cache_sha: 'e9afa2fbe398' vs 'none'
       force_tool: '1' vs '0'
    ≈  analyze_cache: 'eval/results/runs/35b-A/analyze-notes.jsonl' vs 'none'
    ~  git_commit: 'a1b3f71' vs 'f94aac3'
35b-H2      vs 35b-H        83.6 vs  87.2  d= -3.6  disc 10/19  p=0.1360  unresolved rules + NARROWED retrieval (vs rules + bulk)
       index: 'sigs.index.seeded.tsv' vs 'sigs.index.tsv'
       index_sha: 'f5f249b60d4d' vs '2cbb419f6695'
    ~  git_commit: '31a39bf' vs 'a1b3f71'
35b-P1      vs 35b-C        78.4 vs  74.8  d= +3.6  disc 14/ 5  p=0.0636  unresolved message ROLE (system vs user), order held
    ≈  analyze_cache_sha: '1ea6bdf10d23' vs 'e9afa2fbe398'
    ≈  analyze_cache: 'eval/.caches/analyze-cache-P1-35b.jsonl' vs 'eval/results/runs/35b-A/analyze-notes.jsonl'
    ~  git_commit: '1d9fce5' vs 'a1b3f71'
35b-P2      vs 35b-P1       78.4 vs  78.4  d= +0.0  disc 15/15  p=1.0000  unresolved ORDER (before vs after the issue), role held
    ≈  analyze_cache_sha: 'c9a77f0ab3a5' vs '1ea6bdf10d23'
    ≈  analyze_cache: 'eval/.caches/analyze-cache-P2-35b.jsonl' vs 'eval/.caches/analyze-cache-P1-35b.jsonl'
35b-S1      vs 35b-T2       78.0 vs  85.2  d= -7.2  disc  9/27  p=0.0039  RESOLVED   bounded context via a STAGE boundary
    ~  git_commit: '698cd91' vs '1d9fce5'
35b-T2      vs 35b-D        85.2 vs  82.4  d= +2.8  disc 24/17  p=0.3489  unresolved turn decomposition (leather-native, no proxy)
       force_tool: '0' vs '1'
    ~  git_commit: '1d9fce5' vs 'a1b3f71'
35b-T2c     vs 35b-T2       74.8 vs  85.2  d=-10.4  disc  7/33  p=0.0000  RESOLVED   per-turn context clearing (bounded decide, no queue hop)
       analyze_cache: '/home/tyler/git/TGPSKI/leather/examples/14-sig-triage/eval/results/runs/35b-A/analyze-notes.jsonl' vs 'eval/results/runs/35b-A/analyze-notes.jsonl'
    ~  git_commit: '31a39bf' vs '1d9fce5'
35b-T3      vs 35b-T2       78.0 vs  85.2  d= -7.2  disc 10/28  p=0.0051  RESOLVED   decomposition DEPTH (3 turns vs 2)
    ~  git_commit: '0f36f00' vs '1d9fce5'

== 4b ==
4b-A0       vs 4b-A         75.2 vs  77.6  d= -2.4  disc 17/23  p=0.4296  unresolved cost of an offered-but-unused tool
    ≈  analyze_cache_sha: '9e128e497725' vs 'none'
    ≈  analyze_cache: 'eval/results/runs/4b-A/analyze-notes.jsonl' vs 'none'
    ~  git_commit: 'ec70139' vs 'f94aac3'
4b-C        vs 4b-B         67.2 vs  62.8  d= +4.4  disc 23/12  p=0.0895  unresolved catalog content, delivered in the system prompt
4b-C2       vs 4b-C         66.8 vs  67.2  d= -0.4  disc 15/16  p=1.0000  unresolved catalog FORMAT (prose vs term index), delivery held
4b-D        vs 4b-P2        71.6 vs  77.6  d= -6.0  disc 12/27  p=0.0237  RESOLVED   tool round + second forward pass, order held
    ≈  analyze_cache_sha: '9e128e497725' vs 'b7ba0a39e8f2'
       force_tool: '1' vs '0'
    ≈  analyze_cache: 'eval/results/runs/4b-A/analyze-notes.jsonl' vs 'eval/.caches/analyze-cache-P2-4b.jsonl'
    ~  git_commit: '1d9fce5' vs '31a39bf'
4b-Dn       vs 4b-D         72.4 vs  71.6  d= +0.8  disc  6/ 4  p=0.7539  unresolved instruction WORDING, mechanism held
4b-E        vs 4b-D         71.6 vs  71.6  d= +0.0  disc 27/27  p=1.0000  unresolved retrieval payload: narrowed labels vs whole catalog
    ~  git_commit: 'a1b3f71' vs '1d9fce5'
4b-E2       vs 4b-E         69.2 vs  71.6  d= -2.4  disc  7/13  p=0.2632  unresolved hand-written NOT_MATCH boundaries
       index: 'sigs.index.seeded.tsv' vs 'sigs.index.tsv'
       index_sha: 'f5f249b60d4d' vs '2cbb419f6695'
4b-Eauto    vs 4b-E         70.0 vs  71.6  d= -1.6  disc  5/ 9  p=0.4240  unresolved voluntary vs compelled tool use
       force_tool: '0' vs '1'
4b-F        vs 4b-A         80.4 vs  77.6  d= +2.8  disc 27/20  p=0.3817  unresolved the analyze->match STAGE SPLIT
    ~  git_commit: '31a39bf' vs 'f94aac3'
4b-F2       vs 4b-H2        81.6 vs  78.0  d= +3.6  disc 30/21  p=0.2624  unresolved the stage split, rules AND catalog handling held constant
    ≈  analyze_cache_sha: 'none' vs '9e128e497725'
       force_tool: '0' vs '1'
    ≈  analyze_cache: 'none' vs 'eval/results/runs/4b-A/analyze-notes.jsonl'
4b-G        vs 4b-E2        75.6 vs  69.2  d= +6.4  disc 23/ 7  p=0.0052  RESOLVED   retrieval payload: entries vs labels
    ~  git_commit: '31a39bf' vs 'a1b3f71'
4b-G2       vs 4b-G         74.8 vs  75.6  d= -0.8  disc  9/11  p=0.8238  unresolved boundaries enforced in code vs offered as advice
4b-H        vs 4b-A         78.8 vs  77.6  d= +1.2  disc 10/ 7  p=0.6291  unresolved does the catalog ADD to the rules
    ≈  analyze_cache_sha: '9e128e497725' vs 'none'
       force_tool: '1' vs '0'
    ≈  analyze_cache: 'eval/results/runs/4b-A/analyze-notes.jsonl' vs 'none'
    ~  git_commit: '0f36f00' vs 'f94aac3'
4b-H2       vs 4b-H         78.0 vs  78.8  d= -0.8  disc 20/22  p=0.8776  unresolved rules + NARROWED retrieval (vs rules + bulk)
       index: 'sigs.index.seeded.tsv' vs 'sigs.index.tsv'
       index_sha: 'f5f249b60d4d' vs '2cbb419f6695'
    ~  git_commit: '31a39bf' vs '0f36f00'
4b-P1       vs 4b-C         71.6 vs  67.2  d= +4.4  disc 20/ 9  p=0.0614  unresolved message ROLE (system vs user), order held
    ≈  analyze_cache_sha: '4984c8fbc04a' vs '9e128e497725'
    ≈  analyze_cache: 'eval/.caches/analyze-cache-P1-4b.jsonl' vs 'eval/results/runs/4b-A/analyze-notes.jsonl'
    ~  git_commit: '31a39bf' vs 'a1b3f71'
4b-P2       vs 4b-P1        77.6 vs  71.6  d= +6.0  disc 23/ 8  p=0.0107  RESOLVED   ORDER (before vs after the issue), role held
    ≈  analyze_cache_sha: 'b7ba0a39e8f2' vs '4984c8fbc04a'
    ≈  analyze_cache: 'eval/.caches/analyze-cache-P2-4b.jsonl' vs 'eval/.caches/analyze-cache-P1-4b.jsonl'
4b-S1       vs 4b-T2        62.4 vs  77.2  d=-14.8  disc 13/50  p=0.0000  RESOLVED   bounded context via a STAGE boundary
    ~  git_commit: '31a39bf' vs '698cd91'
4b-T2       vs 4b-D         77.2 vs  71.6  d= +5.6  disc 24/10  p=0.0243  RESOLVED   turn decomposition (leather-native, no proxy)
       force_tool: '0' vs '1'
    ~  git_commit: '698cd91' vs '1d9fce5'
4b-T2c      vs 4b-T2        66.0 vs  77.2  d=-11.2  disc 12/40  p=0.0001  RESOLVED   per-turn context clearing (bounded decide, no queue hop)
       analyze_cache: '/home/tyler/git/TGPSKI/leather/examples/14-sig-triage/eval/results/runs/4b-A/analyze-notes.jsonl' vs 'eval/results/runs/4b-A/analyze-notes.jsonl'
    ~  git_commit: '31a39bf' vs '698cd91'
4b-T3       vs 4b-T2        68.0 vs  77.2  d= -9.2  disc  9/32  p=0.0004  RESOLVED   decomposition DEPTH (3 turns vs 2)
    ~  git_commit: '31a39bf' vs '698cd91'
```
