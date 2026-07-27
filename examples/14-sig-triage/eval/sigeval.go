// Command sigeval scores SIG-triage predictions against a labeled corpus and
// gates on configurable thresholds. Deterministic; stdlib only.
//
// Inputs (JSONL):
//   corpus.jsonl       gold: {number, sig, accept?[]}         (accept = other acceptable labels)
//   predictions.jsonl  model: {number, predicted, confidence}
//
// A prediction is CORRECT if predicted ∈ {gold} ∪ accept.
// predicted == "unknown" is an ABSTENTION (tracked separately, not counted as wrong
// unless gold is a concrete SIG and abstention is disallowed for that row).
//
// Usage:
//   go run ./eval/sigeval.go -corpus eval/corpus.jsonl -pred eval/predictions.jsonl \
//       -min-accuracy 0.80 -max-abstain 0.25 -min-core-recall 0.90 \
//       -core sig-network,sig-node,sig-storage,sig-scheduling,sig-apps,sig-api-machinery
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

func main() {
	goldPath := flag.String("gold", "eval/gold.jsonl", "answer-key JSONL {number,sig,accept?}")
	predPath := flag.String("pred", "eval/predictions.jsonl", "predictions JSONL")
	minAcc := flag.Float64("min-accuracy", 0.80, "gate: minimum overall accuracy (excl. abstentions on ambiguous rows)")
	maxAbstain := flag.Float64("max-abstain", 0.30, "gate: maximum abstention rate")
	minCore := flag.Float64("min-core-recall", 0.90, "gate: minimum recall on each core SIG")
	coreCSV := flag.String("core", "sig-network,sig-node,sig-storage,sig-scheduling,sig-apps,sig-api-machinery", "comma-separated core SIGs held to min-core-recall")
	flag.Parse()

	golds, err := readJSONL[gold](*goldPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gold:", err)
		os.Exit(2)
	}
	preds, err := readJSONL[pred](*predPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "predictions:", err)
		os.Exit(2)
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
	var total, correct, abstain, missing int

	for _, g := range golds {
		total++
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
		case acc[p.Predicted]:
			correct++
			perSIG[g.SIG].correct++
			recallHit[g.SIG]++
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
	fmt.Printf("corpus=%d  predicted=%d  missing=%d\n", total, len(preds), missing)
	fmt.Printf("overall accuracy (accept-set, abstain-as-correct where allowed): %.1f%% (%d/%d)\n", 100*accOverall, correct, total)
	fmt.Printf("accuracy on answered (excl. abstentions):                        %.1f%%\n", 100*accOnAnswered)
	fmt.Printf("abstention rate:                                                 %.1f%% (%d/%d)\n", 100*abstainRate, abstain, total)
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
	for _, s := range sigs {
		if !core[s] {
			continue
		}
		rec := 0.0
		if recallTot[s] > 0 {
			rec = float64(recallHit[s]) / float64(recallTot[s])
		}
		chk("core-recall:"+s, rec >= *minCore, fmt.Sprintf("%.0f%%", 100*rec), fmt.Sprintf(">=%.0f%%", 100**minCore))
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
