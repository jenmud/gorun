package llm

import "testing"

func TestWholeRunesHoldsBackAPartCharacter(t *testing.T) {
	euro := []byte("€") // 3 bytes
	for _, tc := range []struct {
		in   []byte
		want int
	}{
		{[]byte("abc"), 3},
		{append([]byte("ab"), euro[:1]...), 2},
		{append([]byte("ab"), euro[:2]...), 2},
		{append([]byte("ab"), euro...), 5},
		{nil, 0},
	} {
		if got := wholeRunes(tc.in); got != tc.want {
			t.Errorf("wholeRunes(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
