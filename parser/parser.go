package parser

import "log/slog"

func Parse(input string) {
	i := yyParse(NewLexer(input))
	slog.Info("parsed input", slog.String("input", input), slog.Int("output", i))
}
