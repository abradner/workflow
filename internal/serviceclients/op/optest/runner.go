// Package optest provides a behaviour-accurate stand-in for the 1Password CLI.
//
// The point of it is that a permissive stub cannot catch an argument-vector
// bug. The op client's original tests asserted only that CreateItem produced
// []string{"item", "create", "-"} and then returned whatever the test wanted,
// so they could not have distinguished a correct invocation from a broken one.
// Any fake that says "yes" to every argv has the same hole.
//
// This one refuses what the real binary refuses. Every rule below was
// established by running the real `op` and recording the result;
// docs/OP_CLI_NOTES.md carries the transcript, the version and the date. Tests
// using this fake need no network, no credentials, and mutate nothing.
//
// What it deliberately does NOT model: whether stdin reaches `op` at all. That
// turns out to be the single most important property of this interface, and it
// is invisible at the argv level - see the "How stdin is delivered" section of
// OP_CLI_NOTES.md. Go's exec.Cmd always hands the child a pipe, which is the
// case that works, so the fake assumes stdin arrives.
//
// The fake is only as good as the observations behind it, and those have a
// shelf life. Re-run the transcript in OP_CLI_NOTES.md when the CLI is
// upgraded and correct anything here that has drifted.
package optest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultVault is where the real CLI puts an item created without --vault.
// Observed to be the account's personal vault.
const DefaultVault = "Private"

// Item is one item held by the fake vault.
type Item struct {
	ID       string
	Title    string
	Category string
	Vault    string
	Sections []any
	Fields   []any
	Note     string
}

// Runner implements the op package's Runner interface against an in-memory
// vault. The zero value is usable and starts empty.
type Runner struct {
	Items []*Item

	// Calls records the argv of every invocation, in order, so a test can
	// assert on how the CLI was driven as well as on the outcome.
	Calls [][]string

	nextID int
}

// Add seeds the fake vault with an item, as though it already existed.
func (r *Runner) Add(item *Item) { r.Items = append(r.Items, item) }

// Find returns the item with the given ID, or nil.
func (r *Runner) Find(id string) *Item {
	for _, it := range r.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (r *Runner) Run(_ context.Context, name string, args []string, stdin []byte) (string, string, error) {
	r.Calls = append(r.Calls, args)

	if name != "op" {
		return "", fmt.Sprintf("%s: command not found", name), fmt.Errorf("exit 127")
	}
	if len(args) < 2 || args[0] != "item" {
		return "", "unknown command", fmt.Errorf("exit 1")
	}

	switch args[1] {
	case "create":
		return r.create(args[2:], stdin)
	case "get":
		return r.get(args[2:])
	default:
		return "", fmt.Sprintf("unknown subcommand %q", args[1]), fmt.Errorf("exit 1")
	}
}

// create models `op item create`.
//
// The category rule is the trap worth encoding: the CLI takes the category
// from EITHER the piped template OR a --category flag, and rejects being given
// both. Since this tool's templates always carry "category", passing the flag
// as well is an error - so "helpfully" adding --category breaks a working call.
//
// Omitting --vault is not an error, but it is rarely what anyone means: the
// item silently lands in the account's default (personal) vault.
func (r *Runner) create(args []string, stdin []byte) (string, string, error) {
	flags, _, err := parseArgs("create", args)
	if err != nil {
		return "", "[ERROR] " + err.Error(), fmt.Errorf("exit 1")
	}

	var tpl struct {
		Title    string `json:"title"`
		Category string `json:"category"`
		Sections []any  `json:"sections"`
		Fields   []any  `json:"fields"`
	}
	if len(stdin) > 0 {
		if err := json.Unmarshal(stdin, &tpl); err != nil {
			return "", "[ERROR] unable to process line 1: invalid item template", fmt.Errorf("exit 1")
		}
	}

	flagCategory := flags["--category"]
	switch {
	case tpl.Category != "" && flagCategory != "":
		return "", "[ERROR] unable to process line 1: cannot provide the item category with both " +
				"the JSON template and the `--category` flag - only specify the category in one location",
			fmt.Errorf("exit 1")
	case tpl.Category == "" && flagCategory == "":
		return "", "[ERROR] provide the item category with '--category' flag", fmt.Errorf("exit 1")
	}

	category := tpl.Category
	if category == "" {
		category = flagCategory
	}

	vault := flags["--vault"]
	if vault == "" {
		vault = DefaultVault
	}

	r.nextID++
	item := &Item{
		ID:       fmt.Sprintf("fakeitemid%06d", r.nextID),
		Title:    tpl.Title,
		Category: normalizeCategory(category),
		Vault:    vault,
		Sections: tpl.Sections,
		// Field IDs supplied in the template are preserved verbatim by the
		// real CLI - that is the basis for stable field identity across runs.
		Fields: tpl.Fields,
	}
	r.Items = append(r.Items, item)

	return item.summaryOutput(), "", nil
}

// summaryOutput reproduces the human-readable block `op item create` prints
// when no output-selection flag is given -- which is what production actually
// receives, since CreateItem passes no --format. It is deliberately not a bare
// ID: returning one would invent a contract the real CLI does not offer and
// invite callers to depend on it.
func (i *Item) summaryOutput() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID:          %s\n", i.ID)
	fmt.Fprintf(&b, "Title:       %s\n", i.Title)
	fmt.Fprintf(&b, "Vault:       %s\n", i.Vault)
	fmt.Fprintf(&b, "Category:    %s\n", i.Category)
	return strings.TrimRight(b.String(), "\n")
}

// Last returns the most recently created item, for tests that need to inspect
// what was written without parsing the CLI's stdout.
func (r *Runner) Last() *Item {
	if len(r.Items) == 0 {
		return nil
	}
	return r.Items[len(r.Items)-1]
}

// get models `op item get <ref> [--vault V] [--format json | --fields F]`.
//
// A miss is exit 1 with a message on stderr, not empty output on exit 0.
func (r *Runner) get(args []string) (string, string, error) {
	flags, positional, err := parseArgs("get", args)
	if err != nil {
		return "", "[ERROR] " + err.Error(), fmt.Errorf("exit 1")
	}
	if len(positional) == 0 {
		return "", "[ERROR] specify an item", fmt.Errorf("exit 1")
	}
	ref, vault := positional[0], flags["--vault"]

	var found *Item
	for _, it := range r.Items {
		if it.ID != ref && it.Title != ref {
			continue
		}
		if vault != "" && it.Vault != vault {
			continue
		}
		found = it
		break
	}
	if found == nil {
		where := "vault"
		if vault != "" {
			where = fmt.Sprintf("%q vault", vault)
		}
		return "", fmt.Sprintf("[ERROR] %q isn't an item in the %s.", ref, where), fmt.Errorf("exit 1")
	}

	if flags["--fields"] == "notesPlain" {
		return found.Note, "", nil
	}

	out, err := json.Marshal(map[string]any{
		"id":       found.ID,
		"title":    found.Title,
		"category": found.Category,
		"vault":    map[string]any{"name": found.Vault},
		"sections": found.Sections,
		"fields":   found.Fields,
	})
	if err != nil {
		return "", "[ERROR] encoding item", fmt.Errorf("exit 1")
	}
	return string(out), "", nil
}

// flagSpec declares which flags each subcommand accepts, and whether each one
// takes a value. Parsing is command-specific because the real CLI is: an
// unknown flag is rejected outright, and a value-taking flag with nothing after
// it is a missing-argument error, not a boolean.
//
// This matters more than it looks. An earlier version accepted any `--flag` and
// treated a valueless one as "true", which meant a typo like `--catgory` was
// silently recorded and ignored — so a test asserting the client's argv would
// still pass against an invocation the real CLI rejects. That is precisely the
// failure this package exists to prevent, reproduced inside the thing meant to
// prevent it.
var flagSpec = map[string]map[string]bool{ // subcommand -> flag -> takes a value
	"create": {
		"--category": true, "--vault": true, "--template": true,
		"--title": true, "--tags": true, "--dry-run": false, "--reveal": false,
	},
	"get": {
		"--vault": true, "--fields": true, "--format": true,
		"--reveal": false, "--include-archive": false,
	},
}

// parseArgs splits argv into flags and positionals for one subcommand,
// rejecting what the real CLI rejects. It is not a general pflag
// reimplementation — only the flags this tool actually passes are modelled.
func parseArgs(sub string, args []string) (map[string]string, []string, error) {
	spec, known := flagSpec[sub]
	if !known {
		return nil, nil, fmt.Errorf("unknown subcommand %q", sub)
	}

	flags := map[string]string{}
	var positional []string

	for i := 0; i < len(args); i++ {
		a := args[i]

		if !strings.HasPrefix(a, "--") {
			positional = append(positional, a) // includes the bare "-"
			continue
		}

		name, inline, hasInline := strings.Cut(a, "=")
		takesValue, ok := spec[name]
		if !ok {
			return nil, nil, fmt.Errorf("unknown flag: %s", name)
		}

		switch {
		case !takesValue:
			if hasInline {
				return nil, nil, fmt.Errorf("flag %s does not take a value", name)
			}
			flags[name] = "true"
		case hasInline:
			flags[name] = inline
		case i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") && args[i+1] != "-":
			flags[name] = args[i+1]
			i++
		default:
			return nil, nil, fmt.Errorf("flag needs an argument: %s", name)
		}
	}
	return flags, positional, nil
}

// normalizeCategory renders a category the way `op item get --format json`
// reports it, regardless of the spelling used to supply it.
func normalizeCategory(s string) string {
	switch strings.ToUpper(strings.ReplaceAll(s, " ", "_")) {
	case "SECURE_NOTE":
		return "SECURE_NOTE"
	case "LOGIN":
		return "LOGIN"
	case "PASSWORD":
		return "PASSWORD"
	default:
		return s
	}
}
