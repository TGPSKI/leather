// Command sigeval scores SIG-triage predictions against a labeled corpus and
// gates on configurable thresholds. Deterministic; stdlib only.
//
// Inputs (JSONL):
//
//	gold.jsonl           gold: {number, sig, accept?[]}   (PRISTINE fetch output — never hand-edited)
//	gold.overrides.jsonl overlay: {number, sig?, accept?[], reason}  (rule-generated relabels; win over gold)
//	splits.jsonl         manifest: {number, tier}         (tier "dev" = tuned on; anything else = held out)
//	predictions.jsonl    model: {number, predicted, confidence}
//
// A prediction is CORRECT if predicted ∈ {gold} ∪ accept.
// predicted == "unknown" is an ABSTENTION (tracked separately, not counted as wrong
// unless gold is a concrete SIG and abstention is disallowed for that row).
//
// Usage:
//
//	go run ./eval/sigeval.go -gold eval/gold.jsonl -pred eval/predictions.jsonl \
//	    -overrides eval/gold.overrides.jsonl -split eval/splits.jsonl \
//	    -min-accuracy 0.80 -max-abstain 0.25 -min-macro-recall 0.85 -min-core-recall 0.90 \
//	    -core sig-network,sig-node,sig-storage,sig-scheduling,sig-apps,sig-api-machinery
//
// Exit 0 = gate passed, 1 = gate failed, 2 = bad input.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type gold struct {
	Number int      `json:"number"`
	SIG    string   `json:"sig"`
	Accept []string `json:"accept"`
}

// override is one row of the gold overrides overlay. The overlay exists so that
// gold.jsonl stays byte-identical to the fetcher's output: relabels (e.g. the
// gold-sanity guard marking a content-free issue as `unknown`) are a diffable,
// reasoned layer on top, not a hand-edit of the answer key. A corpus re-fetch
// can therefore never clobber or silently drift from them.
type override struct {
	Number int      `json:"number"`
	SIG    *string  `json:"sig"`    // nil = leave gold's sig alone
	Accept []string `json:"accept"` // nil = leave gold's accept-set alone
	Reason string   `json:"reason"`
}

// split assigns a row to a tier. "dev" rows are the ones tuning was allowed to
// look at; every other row is held out. Reporting the two separately is the
// real anti-overfitting evidence (eval-iteration-method.md §5.4).
type split struct {
	Number int    `json:"number"`
	Tier   string `json:"tier"`
}

type pred struct {
	Number     int    `json:"number"`
	Predicted  string `json:"predicted"`
	RunnerUp   string `json:"runner_up"`
	Confidence string `json:"confidence"`
}

func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, v)
	}
	return out, sc.Err()
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func acceptable(g gold) map[string]bool {
	s := map[string]bool{g.SIG: true}
	for _, a := range g.Accept {
		s[a] = true
	}
	return s
}

// correctness reduces a prediction set to a per-row verdict under the same
// accept-set rules the main report uses (an allowed `unknown` is correct because
// acceptable() contains it). Shared by the split report and the flip diff so
// those two can never disagree with the headline number.
func correctness(golds []gold, byNum map[int]pred) map[int]bool {
	out := make(map[int]bool, len(golds))
	for _, g := range golds {
		p, ok := byNum[g.Number]
		out[g.Number] = ok && acceptable(g)[p.Predicted]
	}
	return out
}

// normSIG canonicalizes a SIG token: trims, lowercases, and folds the GitHub
// label form "sig/foo" into the catalog form "sig-foo". The match agent is
// asked for the catalog name (sig-foo) but smaller models sometimes emit the
// label form (sig/foo); the two denote the same SIG, so scoring them as a
// mismatch would understate accuracy. "unknown" and empty pass through unchanged.
func normSIG(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "sig/") {
		s = "sig-" + s[len("sig/"):]
	}
	return s
}

func main() {
	goldPath := flag.String("gold", "eval/gold.jsonl", "answer-key JSONL {number,sig,accept?} — pristine fetch output")
	ovrPath := flag.String("overrides", "eval/gold.overrides.jsonl", "gold overrides overlay JSONL {number,sig?,accept?,reason} (optional)")
	splitPath := flag.String("split", "eval/splits.jsonl", "split manifest JSONL {number,tier} (optional; enables the dev/held-out report)")
	catalogPath := flag.String("catalog", "sigs.reference.yaml", "SIG catalog, for the closed-vocabulary check (optional)")
	predPath := flag.String("pred", "eval/predictions.jsonl", "predictions JSONL")
	flipVs := flag.String("flip-vs", "", "prior predictions JSONL: report the per-item flip diff (fixed / regressed / unchanged) against it")
	nullBand := flag.Int("null-band", 6, "flip-diff verdict: |net rows| at or below this is UNRESOLVED. MEASURED, not guessed: two repeat runs of an unchanged config gave net 0 and net -6 on this corpus, so anything within +-6 rows is indistinguishable from doing nothing")
	minAcc := flag.Float64("min-accuracy", 0.80, "gate: minimum overall accuracy (excl. abstentions on ambiguous rows)")
	maxAbstain := flag.Float64("max-abstain", 0.30, "gate: maximum abstention rate")
	minMacroRecall := flag.Float64("min-macro-recall", 0.85, "gate: minimum macro-averaged recall across gold classes (primary per-class health check)")
	minCore := flag.Float64("min-core-recall", 0.90, "gate: minimum recall on each core SIG with support >= -min-class-support")
	minClassSupport := flag.Int("min-class-support", 20, "gate: do not apply the per-class recall floor below this support (small-N noise guard; 0 = always apply)")
	coreCSV := flag.String("core", "sig-network,sig-node,sig-storage,sig-scheduling,sig-apps,sig-api-machinery", "comma-separated core SIGs held to min-core-recall")
	emitRows := flag.String("emit-rows", "", "write per-row verdicts JSONL {number,predicted,gold,correct,abstained} to this path. This is the row-level form of the scorer of record: downstream tools (table, paired comparisons) MUST consume these rows rather than re-deriving correctness, so one scorer produces one number (task #32)")
	flag.Parse()

	golds, err := readJSONL[gold](*goldPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gold:", err)
		os.Exit(2)
	}
	// Duplicate gold numbers silently mis-join between the headline counters
	// (which key off golds in order) and correctByNum/pByNum (which key off
	// number). Fail closed rather than let a later row shadow an earlier one.
	seenNum := map[int]bool{}
	for _, g := range golds {
		if seenNum[g.Number] {
			fmt.Fprintln(os.Stderr, "gold: duplicate number", g.Number)
			os.Exit(2)
		}
		seenNum[g.Number] = true
	}
	if len(golds) == 0 {
		fmt.Fprintln(os.Stderr, "gold: no rows loaded from", *goldPath)
		os.Exit(2)
	}
	// Apply the overrides overlay. Missing file is fine (not every corpus needs
	// one); a malformed file is not — gold hygiene fails closed.
	nOverride := 0
	if *ovrPath != "" {
		ovrs, err := readJSONL[override](*ovrPath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "overrides:", err)
			os.Exit(2)
		}
		byNum := map[int]override{}
		for _, o := range ovrs {
			byNum[o.Number] = o
		}
		for i := range golds {
			o, ok := byNum[golds[i].Number]
			if !ok {
				continue
			}
			if o.SIG != nil {
				golds[i].SIG = *o.SIG
			}
			if o.Accept != nil {
				golds[i].Accept = o.Accept
			}
			nOverride++
		}
	}
	// Split membership: which rows tuning was allowed to see.
	tierOf := map[int]string{}
	if *splitPath != "" {
		sp, err := readJSONL[split](*splitPath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "split:", err)
			os.Exit(2)
		}
		for _, s := range sp {
			tierOf[s.Number] = strings.ToLower(strings.TrimSpace(s.Tier))
		}
	}
	preds, err := readJSONL[pred](*predPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "predictions:", err)
		os.Exit(2)
	}
	// Canonicalize SIG notation (sig/foo -> sig-foo) on both sides so label-form
	// predictions match catalog-form gold and the report stays free of duplicate
	// slash/dash rows for the same SIG.
	for i := range golds {
		golds[i].SIG = normSIG(golds[i].SIG)
		for j := range golds[i].Accept {
			golds[i].Accept[j] = normSIG(golds[i].Accept[j])
		}
	}
	for i := range preds {
		preds[i].Predicted = normSIG(preds[i].Predicted)
	}
	pByNum := map[int]pred{}
	for _, p := range preds {
		pByNum[p.Number] = p
	}
	core := map[string]bool{}
	for _, c := range strings.Split(*coreCSV, ",") {
		if c = strings.TrimSpace(c); c != "" {
			core[c] = true
		}
	}

	type stat struct{ support, correct, abstain int }
	perSIG := map[string]*stat{} // keyed by gold SIG
	recallHit := map[string]int{}
	recallTot := map[string]int{}
	predTot := map[string]int{} // times a concrete SIG was predicted
	predHit := map[string]int{} // of those, times it was acceptable
	confusion := map[string]int{}
	correctByNum := map[int]bool{} // per-row verdict, for the split report
	var total, correct, abstain, missing int

	for _, g := range golds {
		total++
		correctByNum[g.Number] = false
		if _, ok := perSIG[g.SIG]; !ok {
			perSIG[g.SIG] = &stat{}
		}
		perSIG[g.SIG].support++
		recallTot[g.SIG]++
		p, ok := pByNum[g.Number]
		if !ok {
			missing++
			continue
		}
		acc := acceptable(g)
		isConcrete := p.Predicted != "unknown" && !strings.HasPrefix(p.Predicted, "__")
		if isConcrete {
			predTot[p.Predicted]++
		}
		switch {
		case p.Predicted == "unknown" && !acc["unknown"]:
			abstain++
			perSIG[g.SIG].abstain++
		case p.Predicted == "unknown" && acc["unknown"]:
			abstain++
			perSIG[g.SIG].abstain++
			correct++
			perSIG[g.SIG].correct++
			recallHit[g.SIG]++
			correctByNum[g.Number] = true
		case acc[p.Predicted]:
			correct++
			perSIG[g.SIG].correct++
			recallHit[g.SIG]++
			correctByNum[g.Number] = true
			if isConcrete {
				predHit[p.Predicted]++
			}
		default:
			confusion[g.SIG+" -> "+p.Predicted]++
		}
	}
	f1 := func(p, r float64) float64 {
		if p+r == 0 {
			return 0
		}
		return 2 * p * r / (p + r)
	}

	// ---- per-row verdict emission (-emit-rows) ----
	// Written from correctByNum — the same map the split report and flip diff
	// read — so an emitted row can never disagree with the headline number.
	//
	// Written to a temp file in the target's own directory, then renamed over the
	// target: downstream tools trust a fresh mtime as "complete", so a mid-emit
	// failure (disk full, encode error) must never leave a partial file behind at
	// the real path. Same-directory temp file keeps the rename an atomic same-fs
	// move rather than a cross-fs copy.
	if *emitRows != "" {
		dir := filepath.Dir(*emitRows)
		tmp, err := os.CreateTemp(dir, ".sigeval-emit-rows-*.tmp")
		if err != nil {
			fmt.Fprintln(os.Stderr, "emit-rows:", err)
			os.Exit(2)
		}
		tmpPath := tmp.Name()
		failTmp := func(err error) {
			fmt.Fprintln(os.Stderr, "emit-rows:", err)
			_ = tmp.Close() // best-effort cleanup on the exit path
			_ = os.Remove(tmpPath)
			os.Exit(2)
		}
		w := bufio.NewWriter(tmp)
		enc := json.NewEncoder(w)
		for _, g := range golds { // gold order: stable across arms, diffable
			p, ok := pByNum[g.Number]
			row := struct {
				Number    int    `json:"number"`
				Predicted string `json:"predicted"` // post-normSIG; "" = missing
				Gold      string `json:"gold"`
				Correct   bool   `json:"correct"`
				Abstained bool   `json:"abstained"`
			}{g.Number, p.Predicted, g.SIG, correctByNum[g.Number], ok && p.Predicted == "unknown"}
			if err := enc.Encode(row); err != nil {
				failTmp(err)
			}
		}
		if err := w.Flush(); err != nil {
			failTmp(err)
		}
		if err := tmp.Close(); err != nil {
			failTmp(err)
		}
		if err := os.Rename(tmpPath, *emitRows); err != nil {
			fmt.Fprintln(os.Stderr, "emit-rows:", err)
			_ = os.Remove(tmpPath) // best-effort cleanup on the exit path
			os.Exit(2)
		}
	}

	answered := total - abstain - missing
	accOnAnswered := 0.0
	// correct answers that were NOT abstentions / answered set
	nonAbstainCorrect := correct
	for _, g := range golds {
		if p, ok := pByNum[g.Number]; ok && p.Predicted == "unknown" && acceptable(g)["unknown"] {
			nonAbstainCorrect-- // remove abstain-as-correct from the answered accuracy
		}
	}
	if answered > 0 {
		accOnAnswered = float64(nonAbstainCorrect) / float64(answered)
	}
	accOverall := float64(correct) / float64(total)
	abstainRate := float64(abstain) / float64(total)

	// ---- report ----
	fmt.Println("SIG-triage eval")
	fmt.Printf("corpus=%d  predicted=%d  missing=%d  gold-overrides-applied=%d\n", total, len(preds), missing, nOverride)
	fmt.Printf("overall accuracy (accept-set, abstain-as-correct where allowed): %.1f%% (%d/%d)\n", 100*accOverall, correct, total)
	fmt.Printf("accuracy on answered (excl. abstentions):                        %.1f%% (%d/%d)\n", 100*accOnAnswered, nonAbstainCorrect, answered)
	fmt.Printf("abstention rate:                                                 %.1f%% (%d/%d)\n", 100*abstainRate, abstain, total)

	// ---- split report (eval-iteration-method.md §5.4) ----
	// Tuning is allowed to look at the dev slice only. Reporting dev and held-out
	// side by side is what shows the rules generalize rather than memorize: if
	// held-out tracks full, the fixes are principled; if dev >> held-out, they
	// are slice-overfitting and must be rejected.
	if len(tierOf) > 0 {
		type bucket struct{ n, ok int }
		buckets := map[string]*bucket{}
		for _, g := range golds {
			t := tierOf[g.Number]
			if t == "" {
				t = "(untiered)"
			}
			b := buckets[t]
			if b == nil {
				b = &bucket{}
				buckets[t] = b
			}
			b.n++
			if correctByNum[g.Number] {
				b.ok++
			}
		}
		pct := func(b bucket) string {
			if b.n == 0 {
				return "      n/a"
			}
			return fmt.Sprintf("%5.1f%% (%d/%d)", 100*float64(b.ok)/float64(b.n), b.ok, b.n)
		}
		// smoke is the only tier tuning may look at; acceptance is the rest of the
		// gate of record; holdout is never tuned on and never gated on. Printing
		// them in that order reads as increasing evidence of generalization.
		labels := []struct{ tier, note string }{
			{"smoke", "tuned on -- expect the best number here"},
			{"acceptance", "gate of record, not tuned on"},
			{"holdout", "never tuned on <- generalization check"},
			{"dev", "legacy tier name"},
			{"(untiered)", "not in the split manifest"},
		}
		fmt.Println()
		fmt.Println("split by tier:")
		fmt.Printf("  %-12s %s\n", "full:", pct(bucket{total, correct}))
		seen := map[string]bool{}
		for _, l := range labels {
			if b, ok := buckets[l.tier]; ok {
				fmt.Printf("  %-12s %s   %s\n", l.tier+":", pct(*b), l.note)
				seen[l.tier] = true
			}
		}
		for t, b := range buckets {
			if !seen[t] {
				fmt.Printf("  %-12s %s\n", t+":", pct(*b))
			}
		}
	}
	fmt.Println()

	// union of gold classes and predicted classes for a full report
	classSet := map[string]bool{}
	for s := range perSIG {
		classSet[s] = true
	}
	for s := range predTot {
		classSet[s] = true
	}
	sigs := make([]string, 0, len(classSet))
	for s := range classSet {
		if s == "unknown" {
			continue
		}
		sigs = append(sigs, s)
	}
	sort.Strings(sigs)

	fmt.Printf("%-24s %7s %9s %9s %9s %8s\n", "SIG", "support", "precision", "recall", "f1", "abstain")
	fmt.Printf("%-24s %7s %9s %9s %9s %8s\n", strings.Repeat("-", 24), "-------", "---------", "------", "--", "-------")
	var macroP, macroR, macroF float64
	var nClasses int
	var wF, wSup float64
	for _, s := range sigs {
		sup := recallTot[s]
		rec := 0.0
		if sup > 0 {
			rec = float64(recallHit[s]) / float64(sup)
		}
		prec := 0.0
		if predTot[s] > 0 {
			prec = float64(predHit[s]) / float64(predTot[s])
		}
		fsc := f1(prec, rec)
		abst := 0
		if st, ok := perSIG[s]; ok {
			abst = st.abstain
		}
		tag := ""
		if core[s] {
			tag = " *"
		}
		fmt.Printf("%-24s %7d %8.0f%% %8.0f%% %8.0f%% %8d%s\n", s, sup, 100*prec, 100*rec, 100*fsc, abst, tag)
		if sup > 0 { // average over classes that appear in gold
			macroP += prec
			macroR += rec
			macroF += fsc
			nClasses++
			wF += fsc * float64(sup)
			wSup += float64(sup)
		}
	}
	if nClasses > 0 {
		fmt.Printf("%-24s %7s %8.0f%% %8.0f%% %8.0f%%\n", "macro avg", "",
			100*macroP/float64(nClasses), 100*macroR/float64(nClasses), 100*macroF/float64(nClasses))
		fmt.Printf("%-24s %7.0f %8s %8s %8.0f%%\n", "weighted-f1 avg", wSup, "", "", 100*wF/wSup)
	}
	fmt.Println("(* = core SIG held to the recall gate)")

	if len(confusion) > 0 {
		fmt.Println("\ntop confusions (gold -> predicted):")
		type kv struct {
			k string
			v int
		}
		var cs []kv
		for k, v := range confusion {
			cs = append(cs, kv{k, v})
		}
		sort.Slice(cs, func(i, j int) bool {
			if cs[i].v != cs[j].v {
				return cs[i].v > cs[j].v
			}
			return cs[i].k < cs[j].k
		})
		for i, c := range cs {
			if i >= 8 {
				break
			}
			fmt.Printf("  %-40s x%d\n", c.k, c.v)
		}
	}

	// ---- closed-vocabulary check ----
	// Nothing forces a prediction to be a real SIG. The model has emitted
	// `sig-device-plugins` -- plausible, not a SIG (device plugins are sig-node) --
	// which is a name invented from priors rather than read from the catalog. Such
	// a prediction is unroutable by any downstream consumer, so it is worth
	// naming separately from an ordinary misclassification. Checked here rather
	// than asked for in the prompt: this rung is deterministic and free, and
	// prompt budget turned out to be a scarce resource (adding rules degraded
	// unrelated classes).
	if *catalogPath != "" {
		if raw, err := os.ReadFile(*catalogPath); err == nil {
			known := map[string]bool{"unknown": true}
			for _, line := range strings.Split(string(raw), "\n") {
				line = strings.TrimSpace(line)
				if after, ok := strings.CutPrefix(line, "- name:"); ok {
					known[normSIG(strings.TrimSpace(after))] = true
				}
			}
			offVocab := map[string]int{}
			for _, p := range preds {
				if p.Predicted != "" && !known[p.Predicted] {
					offVocab[p.Predicted]++
				}
			}
			if len(offVocab) > 0 {
				names := make([]string, 0, len(offVocab))
				for s, n := range offVocab {
					names = append(names, fmt.Sprintf("%s x%d", s, n))
				}
				sort.Strings(names)
				fmt.Printf("\nOFF-VOCABULARY predictions (not in %s): %s\n",
					*catalogPath, strings.Join(names, ", "))
				fmt.Println("  these are unroutable regardless of correctness — the model invented a SIG name")
			}
		}
	}

	// ---- confidence calibration ----
	// Confidence is only useful if it SEPARATES. A model that stamps `high` on
	// every row gives a confidence router nothing to route on, and voting over it
	// just repeats confident errors. What we want to see: `high` accuracy above
	// the overall number, and the misses concentrated in `medium`/`low`.
	confOrder := []string{"high", "medium", "low"}
	type cstat struct{ n, ok int }
	byConf := map[string]*cstat{}
	for _, c := range confOrder {
		byConf[c] = &cstat{}
	}
	other := &cstat{}
	for _, g := range golds {
		p, ok := pByNum[g.Number]
		if !ok {
			continue
		}
		c := strings.ToLower(strings.TrimSpace(p.Confidence))
		b, known := byConf[c]
		if !known {
			b = other
		}
		b.n++
		if correctByNum[g.Number] {
			b.ok++
		}
	}
	fmt.Println("\nconfidence calibration (accuracy per emitted bucket):")
	fmt.Printf("  %-10s %7s %9s %9s\n", "bucket", "n", "share", "accuracy")
	emit := func(name string, b *cstat) {
		if b.n == 0 {
			return
		}
		fmt.Printf("  %-10s %7d %8.0f%% %8.0f%%\n", name, b.n,
			100*float64(b.n)/float64(total), 100*float64(b.ok)/float64(b.n))
	}
	for _, c := range confOrder {
		emit(c, byConf[c])
	}
	emit("(other)", other)
	if byConf["high"].n == total {
		fmt.Println("  WARNING: confidence is degenerate (100% `high`) — nothing to route on.")
	}

	// Runner-up recovery: of the rows the top pick got wrong, how often was the
	// gold answer the model's own second choice? This is the exact headroom a
	// top-2 adjudicator stage has to work with — if it is near zero, adjudicating
	// between SIG and RUNNER_UP cannot help and the miss is a deeper failure.
	ruTotal, ruRecover, ruMissWithRU := 0, 0, 0
	for _, g := range golds {
		p, ok := pByNum[g.Number]
		if !ok {
			continue
		}
		ru := normSIG(p.RunnerUp)
		if ru != "" && ru != "none" {
			ruTotal++
		}
		if correctByNum[g.Number] {
			continue
		}
		if ru != "" && ru != "none" {
			ruMissWithRU++
			if acceptable(g)[ru] {
				ruRecover++
			}
		}
	}
	if ruTotal > 0 {
		fmt.Printf("  runner-up populated on %d/%d rows\n", ruTotal, total)
		pct := 0.0
		if ruMissWithRU > 0 {
			pct = 100 * float64(ruRecover) / float64(ruMissWithRU)
		}
		fmt.Printf("  runner-up recovery: %d/%d misses (%.0f%%) had gold as the RUNNER_UP"+
			"  <- top-2 adjudicator headroom\n", ruRecover, ruMissWithRU, pct)
	}

	// ---- flip diff ----
	// The unit of iteration feedback (eval-iteration-method.md §4.6): a change that nets positive
	// while regressing rows it used to get right is rejected, not shipped. The
	// aggregate alone hides that trade.
	if *flipVs != "" {
		priors, err := readJSONL[pred](*flipVs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "flip-vs:", err)
			os.Exit(2)
		}
		priorByNum := map[int]pred{}
		for _, p := range priors {
			p.Predicted = normSIG(p.Predicted)
			priorByNum[p.Number] = p
		}
		was := correctness(golds, priorByNum)
		var fixed, regressed []string
		unchanged := 0
		for _, g := range golds {
			b, now := was[g.Number], correctByNum[g.Number]
			switch {
			case !b && now:
				fixed = append(fixed, fmt.Sprintf("#%d %s -> %s (gold %s)",
					g.Number, priorByNum[g.Number].Predicted, pByNum[g.Number].Predicted, g.SIG))
			case b && !now:
				regressed = append(regressed, fmt.Sprintf("#%d %s -> %s (gold %s)",
					g.Number, priorByNum[g.Number].Predicted, pByNum[g.Number].Predicted, g.SIG))
			default:
				unchanged++
			}
		}
		fmt.Printf("\nflip diff vs %s:\n", *flipVs)
		fmt.Printf("  fixed %d / regressed %d / unchanged %d   (net %+d)\n",
			len(fixed), len(regressed), unchanged, len(fixed)-len(regressed))
		for _, s := range fixed {
			fmt.Println("  + " + s)
		}
		for _, s := range regressed {
			fmt.Println("  - " + s)
		}

		// VERDICT is on the AGGREGATE, against the measured null band.
		//
		// Per-class PASS/FAIL was tried as the accept rule and is not sound at this
		// support. Re-running an UNCHANGED prompt produced net -6 rows with
		// sig-node crossing the 90% floor (92% -> 83%) -- the identical signal that
		// had been used to reject a real change. At n=24 one row is 4 points and
		// two rows cross the floor, so a class flips PASS->FAIL on nothing at all.
		// A reject rule that fires on identical inputs is not a rule.
		//
		// So: judge the aggregate against the null band measured from repeat runs
		// of the same configuration, and keep per-class strictly as a DIAGNOSTIC
		// for reading the mechanism (which classes gave rows to which).
		net := len(fixed) - len(regressed)
		switch {
		case net > *nullBand:
			fmt.Printf("  VERDICT: improvement (net %+d, outside the null band of +-%d rows)\n", net, *nullBand)
		case net < -*nullBand:
			fmt.Printf("  VERDICT: regression (net %+d, outside the null band of +-%d rows)\n", net, *nullBand)
		default:
			fmt.Printf("  VERDICT: UNRESOLVED (net %+d is inside the +-%d-row null band; "+
				"repeat runs of one config vary this much)\n", net, *nullBand)
		}

		fmt.Println("\n  per-class recall delta — DIAGNOSTIC ONLY, not a verdict:")
		for _, s := range sigs {
			sup := recallTot[s]
			// sup==0 means s never appears in gold (predicted-only class): recall
			// is undefined there (0/0), not zero, so it must not be diffed.
			if sup == 0 || sup < *minClassSupport {
				continue
			}
			nowR := float64(recallHit[s]) / float64(sup)
			wasHit := 0
			for _, g := range golds {
				if g.SIG == s && was[g.Number] {
					wasHit++
				}
			}
			wasR := float64(wasHit) / float64(sup)
			d := 100 * (nowR - wasR)
			if d == 0 {
				continue
			}
			// One row is worth this many points here -- the scale that makes a
			// per-class swing look dramatic when it is a single item.
			perRow := 100.0 / float64(sup)
			note := ""
			if abs(d) <= 2*perRow+0.01 {
				note = fmt.Sprintf("  (<=2 rows at n=%d; inside noise)", sup)
			}
			fmt.Printf("    %-24s %3.0f%% -> %3.0f%%  (%+.0f pts)%s\n", s, 100*wasR, 100*nowR, d, note)
		}
	}

	// ---- gate ----
	fmt.Println("\ngate:")
	pass := true
	chk := func(name string, ok bool, got, want string) {
		status := "PASS"
		if !ok {
			status = "FAIL"
			pass = false
		}
		fmt.Printf("  [%s] %-28s got %s (threshold %s)\n", status, name, got, want)
	}
	chk("overall-accuracy", accOverall >= *minAcc, fmt.Sprintf("%.1f%%", 100*accOverall), fmt.Sprintf(">=%.0f%%", 100**minAcc))
	chk("abstention-rate", abstainRate <= *maxAbstain, fmt.Sprintf("%.1f%%", 100*abstainRate), fmt.Sprintf("<=%.0f%%", 100**maxAbstain))

	// Macro-recall is the PRIMARY per-class health check: it averages recall over
	// gold classes, so a class the model systematically ignores still drags the
	// gate down, but a single defensible confusion in one class does not.
	//
	// It is averaged over the SAME classes the per-class floor applies to. A
	// singleton class scores 0% or 100% and nothing else, so including tiny
	// classes lets a handful of 1-row classes swing the headline gate: on the
	// 250-row corpus, three singletons at 100% inflated it from 86% to 89% and
	// would have masked a genuine 3-point per-class regression.
	macroRecall, macroN := 0.0, 0
	for _, s := range sigs {
		// A class with recallTot[s]==0 never appears in gold (it is predicted-only,
		// e.g. an off-vocabulary or mislabeled prediction landing in classSet via
		// predTot). Its recall is undefined (0/0), not zero; skip it unconditionally
		// so -min-class-support 0 cannot poison the macro sum to NaN.
		if recallTot[s] == 0 || recallTot[s] < *minClassSupport {
			continue
		}
		macroRecall += float64(recallHit[s]) / float64(recallTot[s])
		macroN++
	}
	gatedNote := fmt.Sprintf("over %d classes with support>=%d", macroN, *minClassSupport)
	if macroN == 0 {
		// No class clears the support bar: fall back to every gold class rather
		// than silently gating on nothing, and say so.
		macroRecall, macroN = macroR, nClasses
		gatedNote = fmt.Sprintf("over all %d gold classes -- NO class reaches support %d", nClasses, *minClassSupport)
	}
	if macroN > 0 {
		macroRecall /= float64(macroN)
	}
	chk("macro-recall", macroRecall >= *minMacroRecall, fmt.Sprintf("%.0f%%", 100*macroRecall), fmt.Sprintf(">=%.0f%%", 100**minMacroRecall))
	fmt.Printf("         %s\n", gatedNote)

	// Per-class recall floors are only meaningful where support can carry them.
	// At n=10 and p=0.85 the standard error on recall is ~11 points, so a 90%
	// floor at that support gates on sampling noise: one defensible confusion is
	// -8 to -14 points and flips the verdict. Below -min-class-support the class
	// is reported for information and excluded from the gate; the fix for a
	// low-support class is more corpus, not a lower threshold (LEP-0006 §11).
	var skipped []string
	for _, s := range sigs {
		if !core[s] {
			continue
		}
		sup := recallTot[s]
		rec := 0.0
		if sup > 0 {
			rec = float64(recallHit[s]) / float64(sup)
		}
		if sup < *minClassSupport {
			skipped = append(skipped, fmt.Sprintf("%s (support %d)", s, sup))
			fmt.Printf("  [INFO] %-28s got %.0f%% (not gated: support %d < %d)\n", "core-recall:"+s, 100*rec, sup, *minClassSupport)
			continue
		}
		chk("core-recall:"+s, rec >= *minCore, fmt.Sprintf("%.0f%%", 100*rec), fmt.Sprintf(">=%.0f%%", 100**minCore))
	}
	if len(skipped) > 0 {
		fmt.Printf("  note: %d core class(es) below the min-support guard, held to macro-recall only: %s\n",
			len(skipped), strings.Join(skipped, ", "))
	}
	if missing > 0 {
		chk("coverage", false, fmt.Sprintf("%d missing", missing), "0 missing")
	}

	if pass {
		fmt.Println("\nGATE PASSED")
		os.Exit(0)
	}
	fmt.Println("\nGATE FAILED")
	os.Exit(1)
}
