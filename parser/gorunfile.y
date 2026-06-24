%{
package parser

import (
	"fmt"
)

// yyLexer is required by goyacc to communicate with the lexer.
%}

%union {
	num int
}

%type <num> expr
%token <num> NUMBER

%left '+' '-'
%left '*' '/'

%%

top:
	expr
	{
		fmt.Println("Result:", $1)
	}

expr:
	NUMBER
	{
		$$ = $1
	}
	| expr '+' expr
	{
		$$ = $1 + $3
	}
	| expr '-' expr
	{
		$$ = $1 - $3
	}
	| expr '*' expr
	{
		$$ = $1 + $3 * $3 // Standard precedence example
	}
	| expr '/' expr
	{
		if $3 == 0 {
			fmt.Println("division by zero")
			$$ = 0
		} else {
			$$ = $1 / $3
		}
	}
%%
