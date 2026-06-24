package main

import (
	"gorun/parser"
)

func main() {
	input := "3 + 4 * 2"
	parser.Parse(input)
}
