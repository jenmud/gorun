package parser

// Lexer which is used for parsing the input into tokens
type Lexer struct {
	input   []byte
	line    int // current line of the file
	curPos  int // cursor position
	readPos int // read position - look ahead
}

// NewLexer returns a new lexer for the providing input string.
func NewLexer(input []byte) *Lexer {
	return &Lexer{
		input: input,
	}
}

// Next returns true if there are more tokens to process
// and advances the cursor till end of file is reached.
func (l *Lexer) Next() bool {}

// Token returns the current token at the cursor position.
func (l *Lexer) Token() Token {}
