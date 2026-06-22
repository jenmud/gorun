package parser

// Lexer which is used for parsing the input into tokens
type Lexer struct {
	input   string
	line    int  // current line of the file
	curPos  int  // cursor position
	readPos int  // read position - look ahead
	ch      byte // current read charator
}

// NewLexer returns a new lexer for the providing input string.
func NewLexer(input string) *Lexer {
	l := &Lexer{
		input: input,
	}

	// advance the everything to the first char in the input
	l.readChar()
	return l
}

// readChar will return the current charactor and advance the cursor.
// Rune `0` indicates EOF.
func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
		return
	}

	l.ch = l.input[l.readPos]
	l.curPos = l.readPos
	l.readPos++
}

func (l *Lexer) NextToken() Token {
	var t Token

	switch l.ch {
	case ':':
		t.Type = COLON
		t.Value = append(t.Value, l.ch)
	default:
		t.Type = ERR
		t.Value = []byte("unexpected parsing error")
	}

	return t
}
