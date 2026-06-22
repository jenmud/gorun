package parser

// Lexer which is used for parsing the input into tokens
type Lexer struct {
	input   string
	line    int // current line of the file
	curPos  int // cursor position
	readPos int // read position - look ahead
}

// NewLexer returns a new lexer for the providing input string.
func NewLexer(input string) *Lexer {
	return &Lexer{
		input: input,
	}
}

// readChar will return the current charactor and advance the cursor.
// Rune `0` indicates EOF.
func (l *Lexer) readChar() rune {
	if l.readPos >= len(l.input) {
		return 0
	}

	ch := l.input[l.readPos]
	l.curPos = l.readPos
	l.readPos++
	return rune(ch)
}
