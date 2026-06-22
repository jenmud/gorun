package parser

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/suite"
)

type LexerTestSuite struct {
	suite.Suite
	lexer *Lexer
}

func (suite *LexerTestSuite) TestLexing__readChar() {
	input := "ab"

	l := NewLexer(input)
	suite.NotNil(l)

	// don't need to call .readChar here because
	// when we create the lexer it will automatically
	// advance the pointer to the firat char in the input
	suite.Equal(byte('a'), l.ch)

	l.readChar()
	suite.Equal(byte('b'), l.ch)

	l.readChar()
	suite.Equal(byte(0), l.ch)
}

func (suite *LexerTestSuite) TestLexing__NextToken() {
	input := ":"

	l := NewLexer(input)
	suite.NotNil(l)

	got := l.NextToken()
	want := Token{Type: COLON, Value: []byte(":")}

	if diff := cmp.Diff(want, got); diff != "" {
		suite.T().Errorf("%s() mismatch (-want +got):\n%s", suite.T().Name(), diff)
	}
}

//func (suite *LexerTestSuite) _TestLexing__Single_task() {
//	input := []byte(`
//	hostname:
//		echo $HOST
//	`)
//
//	lexer := NewLexer(input)
//	suite.NotNil(lexer)
//
//	want := []Token{
//		Token{Type: IDENTIFIER, Value: []byte("hostname")},
//		Token{Type: COLON, Value: []byte(":")},
//		Token{Type: CMD, Value: []byte("echo $HOST")},
//		Token{Type: EOF, Value: nil},
//	}
//
//	got := []Token{}
//
//	for suite.lexer.Next() {
//		got = append(got, suite.lexer.Token())
//	}
//
//	suite.ElementsMatch(want, got)
//}

func TestLexingTestSuite(t *testing.T) {
	suite.Run(t, new(LexerTestSuite))
}
