// Package docs embeds gorun's user documentation, the Markdown files in this folder, and finds
// the sections that answer a question, for the web UI's assistant.
package docs

import (
	"embed"
	"io/fs"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
)

//go:embed *.md
var files embed.FS

// Section is one part of a doc under a heading. Title names the file and its headings, such as
// "web.md: The web UI > Assistant".
type Section struct {
	Title, Text string
	terms       map[string]int // with the title's words counted twice
	length      int
}

// maxSection is the longest a section gets before it is split at a paragraph, so a hit is short.
const maxSection = 1400

var index = sync.OnceValue(load)

type corpus struct {
	sections []Section
	df       map[string]int // how many sections have each term
	avgLen   float64
}

func load() corpus {
	var c corpus
	names, _ := fs.Glob(files, "*.md")
	for _, name := range names {
		data, _ := files.ReadFile(name)
		c.sections = append(c.sections, split(name, string(data))...)
	}
	c.df = map[string]int{}
	total := 0
	for i := range c.sections {
		s := &c.sections[i]
		s.terms = map[string]int{}
		for _, t := range terms(s.Title + " " + s.Title + " " + s.Text) {
			s.terms[t]++
			s.length++
		}
		for t := range s.terms {
			c.df[t]++
		}
		total += s.length
	}
	c.avgLen = float64(total) / float64(max(len(c.sections), 1))
	return c
}

var heading = regexp.MustCompile(`^(#{1,3}) (.+)$`)

// split cuts a doc into sections at its #, ## and ### headings, outside code blocks.
func split(name, text string) []Section {
	var out []Section
	var path [3]string
	var body []string
	flush := func() {
		title := name + ": " + strings.Join(slices.DeleteFunc(slices.Clone(path[:]), func(s string) bool { return s == "" }), " > ")
		for _, part := range chunks(strings.TrimSpace(strings.Join(body, "\n"))) {
			out = append(out, Section{Title: title, Text: part})
		}
		body = nil
	}
	code := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") {
			code = !code
		}
		if m := heading.FindStringSubmatch(line); m != nil && !code {
			flush()
			level := len(m[1]) - 1
			path[level] = m[2]
			for i := level + 1; i < len(path); i++ {
				path[i] = ""
			}
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}

// chunks splits text at blank lines into parts of at most about maxSection characters.
func chunks(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		if cur.Len() > 0 && cur.Len()+len(para) > maxSection {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	return append(out, cur.String())
}

var word = regexp.MustCompile(`-{0,2}[a-z0-9_][a-z0-9_.:/-]*[a-z0-9_]|[a-z0-9]`)

// stop are words too common to say what a question is about.
var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be but by can do does for from how i if in into is it its
		me my of on or so that the then there these this to was what when where which who why will with you your
		i'm it's i've there's what's don't doesn't can't want need use using get set make way should could would
		gorun`) {
		stop[w] = true
	}
}

// terms are the lowercase words of text worth matching, without stop words.
func terms(text string) []string {
	var out []string
	for _, w := range word.FindAllString(strings.ToLower(text), -1) {
		w = strings.Trim(w, ".:/")
		if w != "" && !stop[w] {
			out = append(out, stem(w))
		}
	}
	return out
}

// stem cuts a plain word's common English endings, so "installs" and "installed" match
// "install". Flags, keys and paths are kept as they are.
func stem(w string) string {
	if strings.ContainsAny(w, "-_./:0123456789") || len(w) < 5 {
		return w
	}
	for _, suf := range []string{"ing", "ed", "es", "s"} {
		if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 4 {
			return w[:len(w)-len(suf)]
		}
	}
	return w
}

// Search returns the sections that best match question, best first: at most n of them, about
// budget characters in all, and only ones that share at least two terms with it, or one rare
// term, such as a flag or a key.
func Search(question string, n, budget int) []Section {
	c := index()
	q := slices.Compact(slices.Sorted(slices.Values(terms(question))))
	if len(q) == 0 {
		return nil
	}
	type hit struct {
		i       int
		score   float64
		matched int
	}
	var hits []hit
	N := float64(len(c.sections))
	for i, s := range c.sections {
		var score float64
		matched := 0
		for _, t := range q {
			f := float64(s.terms[t])
			if f == 0 {
				continue
			}
			matched++
			idf := math.Log(1 + (N-float64(c.df[t])+0.5)/(float64(c.df[t])+0.5))
			// BM25 with the usual k1 = 1.2 and b = 0.75
			score += idf * f * 2.2 / (f + 1.2*(0.25+0.75*float64(s.length)/c.avgLen))
		}
		if matched >= 2 || score > 3 {
			hits = append(hits, hit{i, score, matched})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return cmpFloat(b.score, a.score) })
	var out []Section
	size := 0
	for _, h := range hits {
		s := c.sections[h.i]
		if len(out) == n || size+len(s.Text) > budget && len(out) > 0 {
			break
		}
		if h.score < 0.5*hits[0].score {
			break // far behind the best, so likely off the point
		}
		out = append(out, s)
		size += len(s.Text)
	}
	return out
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
