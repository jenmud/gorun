package parser

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type LexerTestSuite struct {
	suite.Suite
	lexer *Lexer
}

func (suite *LexerTestSuite) TestLexing__Next__empty_string() {
	input := []byte{}

	l := NewLexer(input)
	suite.NotNil(l)

	suite.False(l.Next())
	suite.Equal(0, l.curPos)
	suite.Equal(0, l.readPos)
}

func (suite *LexerTestSuite) TestLexing__Next__longer_example() {
	input := []byte(`
		hostname:
			echo "$HOST"
	`)

	l := NewLexer(input)
	suite.NotNil(l)

	// test starts at cursor position 1 and read position 2
	// because when you call .Next() it has alrady advanced
	c := 1
	r := 2
	for l.Next() {
		suite.Equal(c, l.curPos)
		suite.Equal(r, l.readPos)
		c++
		r++
	}
}

func (suite *LexerTestSuite) TestLexing__Next() {
	input := []byte(`ab`)

	l := NewLexer(input)
	suite.NotNil(l)

	suite.True(l.Next())
	suite.Equal(1, l.curPos)
	suite.Equal(2, l.readPos)

	suite.True(l.Next())
	suite.Equal(2, l.curPos)
	suite.Equal(3, l.readPos)

	suite.False(l.Next())
	suite.Equal(2, l.curPos)
	suite.Equal(3, l.readPos)
}

func (suite *LexerTestSuite) _TestLexing__Single_task() {
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
