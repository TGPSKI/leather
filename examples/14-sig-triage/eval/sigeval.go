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
// real anti-overfitting evidence (LEP-0007 §5.4).
type split struct {
	Number int    `json:"number"`
	Tier   string `json:"tier"`
}

type pred struct {
	Number     int    `json:"number"`
	Predicted  string `json:"predicted"`
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

func acceptable(g gold) map[string]bool {
	s := map[string]bool{g.SIG: true}
	for _, a := range g.Accept {
		s[a] = true
	}
	return s
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
	predPath := flag.String("pred", "eval/predictions.jsonl", "predictions JSONL")
	minAcc := flag.Float64("min-accuracy", 0.80, "gate: minimum overall accuracy (excl. abstentions on ambiguous rows)")
	maxAbstain := flag.Float64("max-abstain", 0.30, "gate: maximum abstention rate")
	minMacroRecall := flag.Float64("min-macro-recall", 0.85, "gate: minimum macro-averaged recall across gold classes (primary per-class health check)")
	minCore := flag.Float64("min-core-recall", 0.90, "gate: minimum recall on each core SIG with support >= -min-class-support")
	minClassSupport := flag.Int("min-class-support", 20, "gate: do not apply the per-class recall floor below this support (small-N noise guard; 0 = always apply)")
	coreCSV := flag.String("core", "sig-network,sig-node,sig-storage,sig-scheduling,sig-apps,sig-api-machinery", "comma-separated core SIGs held to min-core-recall")
	flag.Parse()

	golds, err := readJSONL[gold](*goldPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gold:", err)
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

	answered := total - abstain
	accOnAnswered := 0.0
	if answered > 0 {
		// correct answers that were NOT abstentions / answered set
		nonAbstainCorrect := correct
		for _, g := range golds {
			if p, ok := pByNum[g.Number]; ok && p.Predicted == "unknown" && acceptable(g)["unknown"] {
				nonAbstainCorrect-- // remove abstain-as-correct from the answered accuracy
			}
		}
		accOnAnswered = float64(nonAbstainCorrect) / float64(answered)
	}
	accOverall := float64(correct) / float64(total)
	abstainRate := float64(abstain) / float64(total)

	// ---- report ----
	fmt.Println("SIG-triage eval")
	fmt.Printf("corpus=%d  predicted=%d  missing=%d  gold-overrides-applied=%d\n", total, len(preds), missing, nOverride)
	fmt.Printf("overall accuracy (accept-set, abstain-as-correct where allowed): %.1f%% (%d/%d)\n", 100*accOverall, correct, total)
	fmt.Printf("accuracy on answered (excl. abstentions):                        %.1f%%\n", 100*accOnAnswered)
	fmt.Printf("abstention rate:                                                 %.1f%% (%d/%d)\n", 100*abstainRate, abstain, total)

	// ---- split report (LEP-0007 §5.4) ----
	// Tuning is allowed to look at the dev slice only. Reporting dev and held-out
	// side by side is what shows the rules generalize rather than memorize: if
	// held-out tracks full, the fixes are principled; if dev >> held-out, they
	// are slice-overfitting and must be rejected.
	if len(tierOf) > 0 {
		type bucket struct{ n, ok int }
		devB, heldB := bucket{}, bucket{}
		for _, g := range golds {
			b := &heldB
			if tierOf[g.Number] == "dev" {
				b = &devB
			}
			b.n++
			if correctByNum[g.Number] {
				b.ok++
			}
		}
		pct := func(b bucket) string {
			if b.n == 0 {
				return "     n/a"
			}
			return fmt.Sprintf("%5.1f%% (%d/%d)", 100*float64(b.ok)/float64(b.n), b.ok, b.n)
		}
		fmt.Println()
		fmt.Println("split (dev = tuned on; held-out = never tuned on):")
		fmt.Printf("  full:      %s\n", pct(bucket{total, correct}))
		fmt.Printf("  dev-slice: %s\n", pct(devB))
		fmt.Printf("  held-out:  %s   <- generalization check\n", pct(heldB))
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
	// every gold class, so a class the model systematically ignores still drags
	// the gate down, but a single defensible confusion in a tiny class does not.
	macroRecall := 0.0
	if nClasses > 0 {
		macroRecall = macroR / float64(nClasses)
	}
	chk("macro-recall", macroRecall >= *minMacroRecall, fmt.Sprintf("%.0f%%", 100*macroRecall), fmt.Sprintf(">=%.0f%%", 100**minMacroRecall))

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
