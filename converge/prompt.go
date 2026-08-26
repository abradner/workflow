package converge

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Prompter answers Questions interactively - the Go equivalent of the
// original Ruby tool's Utils::InteractivePrompt, with the same contract:
// confirm questions take a default on bare Enter, value questions are
// skippable, and a Reason renders as a warning before the prompt.
//
// In and Out are injectable so tests never touch the process's stdin.
type Prompter struct {
	In  io.Reader
	Out io.Writer
}

// NewPrompter builds a Prompter on the process's stdin/stdout.
func NewPrompter() *Prompter {
	return &Prompter{In: os.Stdin, Out: os.Stdout}
}

// Resolve asks every question in order and returns one Answer per Question.
// An input error (EOF, closed pipe) aborts with what remains unresolved as
// an *UnresolvedError - Ctrl+D mid-session behaves like non-interactive
// mode rather than fabricating answers.
func (p *Prompter) Resolve(questions []Question) ([]Answer, error) {
	scanner := bufio.NewScanner(p.In)
	answers := make([]Answer, 0, len(questions))

	for i, q := range questions {
		if q.Reason != "" {
			fmt.Fprintf(p.Out, "\n  ⚠ ISSUE: %s\n", q.Reason)
			if q.Hint != "" {
				fmt.Fprintf(p.Out, "  You can answer below, or %s.\n", q.Hint)
			}
		}

		switch q.Style {
		case Confirm:
			suffix := "[y/N]"
			if q.Default {
				suffix = "[Y/n]"
			}
			fmt.Fprintf(p.Out, "  ? %s %s > ", q.Prompt, suffix)

			line, ok := readLine(scanner)
			if !ok {
				return answers, &UnresolvedError{Questions: questions[i:]}
			}
			confirmed := q.Default
			if line != "" {
				confirmed = strings.HasPrefix(strings.ToLower(line), "y")
			}
			answers = append(answers, Answer{Question: q, Confirmed: confirmed})

		case Value:
			fmt.Fprintf(p.Out, "  %s > ", q.Prompt)

			line, ok := readLine(scanner)
			if !ok {
				return answers, &UnresolvedError{Questions: questions[i:]}
			}
			answers = append(answers, Answer{Question: q, Value: line})
		}
	}

	return answers, nil
}

// readLine returns the next trimmed line, reporting false on EOF or error.
func readLine(scanner *bufio.Scanner) (string, bool) {
	if !scanner.Scan() {
		return "", false
	}
	return strings.TrimSpace(scanner.Text()), true
}
