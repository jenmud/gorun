package parser

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type LexerTestSuite struct {
	suite.Suite
	lexer *Lexer
}

func (suite *LexerTestSuite) TestLexing__Single_task() {
	input := []byte(`
	hostname:
		echo $HOST
	`)

	lexer := NewLexer(input)
	suite.NotNil(lexer)

	want := []Token{
		Token{Type: IDENTIFIER, Value: []byte("hostname")},
		Token{Type: COLON, Value: []byte(":")},
		Token{Type: CMD, Value: []byte("echo $HOST")},
		Token{Type: EOF, Value: nil},
	}

	got := []Token{}

	for suite.lexer.Next() {
		got = append(got, suite.lexer.Token())
	}

	suite.ElementsMatch(want, got)
}

func TestLexingTestSuite(t *testing.T) {
	suite.Run(t, new(LexerTestSuite))
}
