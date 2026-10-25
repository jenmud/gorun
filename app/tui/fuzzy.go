package main

import (
	"unicode"
)

// fuzzy match scoring, loosely modelled on fzf: every pattern character must
// appear in the text in order, and matches score higher when they are
// consecutive, start a word, or start the text.
const (
	scoreMatch       = 16
	bonusBoundary    = 10 // match starts a word: start of text, after - _ . / or space, or a camelCase hump
	bonusConsecutive = 8  // match directly follows the previous match
	bonusFirstChar   = 6  // match is the very first character of the text
	penaltyGapStart  = 3  // first skipped character between two matches
	penaltyGap       = 1  // every further skipped character
)

// fuzzyMatch reports whether every rune of pattern appears in text in order, ignoring
// case. It returns a score (higher is a better match) and the rune indexes of text
// that matched, for highlighting. An empty pattern matches everything with score 0.
func fuzzyMatch(pattern, text string) (score int, positions []int, ok bool) {
	p := []rune(pattern)
	t := []rune(text)

	if len(p) == 0 {
		return 0, nil, true
	}

	// forward pass: find the earliest index where the whole pattern has matched
	pi, end := 0, -1
	for ti := range t {
		if fold(t[ti]) == fold(p[pi]) {
			pi++
			if pi == len(p) {
				end = ti
				break
			}
		}
	}
	if end < 0 {
		return 0, nil, false
	}

	// backward pass: from that end, walk back to find the shortest window holding the
	// pattern, e.g. "ab" in "a-x-ab" picks the final "ab" rather than the first "a"
	positions = make([]int, len(p))
	pi = len(p) - 1
	for ti := end; ti >= 0 && pi >= 0; ti-- {
		if fold(t[ti]) == fold(p[pi]) {
			positions[pi] = ti
			pi--
		}
	}

	for i, pos := range positions {
		score += scoreMatch

		if isBoundary(t, pos) {
			score += bonusBoundary
		}
		if pos == 0 {
			score += bonusFirstChar
		}

		if i > 0 {
			gap := pos - positions[i-1] - 1
			if gap == 0 {
				score += bonusConsecutive
			} else {
				score -= penaltyGapStart + (gap-1)*penaltyGap
			}
		}
	}

	return score, positions, true
}

func fold(r rune) rune {
	return unicode.ToLower(r)
}

// isBoundary reports whether t[i] starts a word.
func isBoundary(t []rune, i int) bool {
	if i == 0 {
		return true
	}

	prev, cur := t[i-1], t[i]
	switch {
	case !unicode.IsLetter(prev) && !unicode.IsDigit(prev):
		return unicode.IsLetter(cur) || unicode.IsDigit(cur)
	case unicode.IsLower(prev) && unicode.IsUpper(cur):
		return true
	case unicode.IsLetter(prev) && unicode.IsDigit(cur):
		return true
	}

	return false
}
