package parser

// TokenType is the type of token
type TokenType int

const (
	EOF        TokenType = iota // 0
	ERR                         // 1
	KEYWORD                     // built in keywords
	COLON                       // :
	IDENTIFIER                  // task name, etc...
	CMD                         // command line
)

// Token is the token parsed which is the type and the raw value
type Token struct {
	Type  TokenType
	Value []byte
}
