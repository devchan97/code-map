package lexical

import (
	"math"
	"testing"
)

func TestScore_ZeroCorpusReturnsZero(t *testing.T) {
	if got := Score(1, 1, 10, 10, 0, BM25K1, BM25B); got != 0 {
		t.Errorf("Score with n=0 returned %v; want 0", got)
	}
	if got := Score(1, 1, 10, 0, 5, BM25K1, BM25B); got != 0 {
		t.Errorf("Score with avgLen=0 returned %v; want 0", got)
	}
}

func TestScore_TFMonotonic(t *testing.T) {
	// More occurrences of the same term should not decrease the score
	// (all else equal).
	low := Score(1, 1, 10, 10, 100, BM25K1, BM25B)
	high := Score(5, 1, 10, 10, 100, BM25K1, BM25B)
	if !(high >= low) {
		t.Errorf("BM25 should be monotonic in tf: tf=1 → %v, tf=5 → %v", low, high)
	}
}

func TestScore_RareTermHigherIDF(t *testing.T) {
	// A term that appears in fewer documents should score higher (higher IDF).
	rare := Score(1, 1, 10, 10, 100, BM25K1, BM25B)
	common := Score(1, 50, 10, 10, 100, BM25K1, BM25B)
	if !(rare > common) {
		t.Errorf("Rare term IDF should exceed common: rare=%v common=%v", rare, common)
	}
}

func TestScore_LongDocPenalised(t *testing.T) {
	// At equal tf, a longer-than-average document is penalised.
	avg := Score(2, 1, 10, 10, 100, BM25K1, BM25B)
	long := Score(2, 1, 100, 10, 100, BM25K1, BM25B)
	if !(avg > long) {
		t.Errorf("Long doc should score lower: avg=%v long=%v", avg, long)
	}
}

func TestScore_FormulaSpotCheck(t *testing.T) {
	// Hand-computed value for tf=2, df=1, docLen=avgLen=10, n=100.
	got := Score(2, 1, 10, 10, 100, 1.5, 0.75)
	idf := math.Log((100.0-1.0+0.5)/(1.0+0.5) + 1.0)
	normTF := 2.0 * (1.5 + 1) / (2.0 + 1.5*(1-0.75+0.75*1.0))
	want := idf * normTF
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("Score = %v; want %v", got, want)
	}
}
