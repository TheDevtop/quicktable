package main

/*
	Quicktable REPL

	An interactive shell on the Mustang wide-column engine.
	Reads commands from a line editor on a terminal, or one per line
	from standard input otherwise, so it can be scripted.
*/

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/dgraph-io/badger/v4"
	"github.com/mattn/go-isatty"

	"github.com/TheDevtop/quicktable/internal/mustang"
)

const usage = `commands:
  tables                                  list tables
  rows    <t>                             list rowKeys of t
  cols    <t>                             list columns of t
  get     <t> [r [c]]                     get table, row or value
  column  <t> <c>                         get column c of every row
  mget    <t> <r> <c> [<t> <r> <c>...]    get values from one snapshot
  set     <t> <r> <c> <v> [<c> <v>...]    set values
  incr    <t> <r> <c> [delta]             add delta (default 1) to a number
  del     <t> <r> [c]                     delete row, or one value
  dropcol <t> <c>                         delete column c from every row
  drop    <t>                             delete table
  query   <t> [clauses]                   matching rows
  count   <t> [clauses]                   number of matching rows
  sum|avg|min|max <t> <c> [clauses]       aggregate c over matching rows
  begin                                   start a batch
  if      <t> <r> <c> <op> [v]            add a condition to the batch
  commit | abort                          end the batch
  help | quit

clauses:
  prefix <p>  from <k>  to <k>  limit <n>  reverse  select <c>...
  where <c> <op> [v]   ops: = != < <= > >= prefix contains exists missing

values: 30 is a number, true/false a bool, anything else a string;
  "quoted" is always a string.
`

var (
	keyStyle = lipgloss.NewStyle().Bold(true)
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	dimStyle = lipgloss.NewStyle().Faint(true)
)

var ops = map[string]mustang.Op{
	"=": mustang.Eq, "!=": mustang.Ne,
	"<": mustang.Lt, "<=": mustang.Le,
	">": mustang.Gt, ">=": mustang.Ge,
	"prefix": mustang.Prefix, "contains": mustang.Contains,
	"exists": mustang.Exists, "missing": mustang.Missing,
}

var (
	errArgs = errors.New("wrong arguments, see help")
	errQuit = errors.New("quit")
)

// token is one word of a command; quoted words are always strings.
type token struct {
	s      string
	quoted bool
}

type repl struct {
	s     *mustang.Store
	batch *mustang.Batch // pending batch; nil outside begin/commit
	nops  int            // ops in batch
	tty   bool
	out   io.Writer
}

func main() {
	os.Exit(run())
}

func run() int {
	dir := flag.String("d", "./data", "Storage directory")
	mem := flag.Bool("m", false, "Run database completely in-memory")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: qt [-d dir] [-m]")
		return 2
	}

	opt := badger.DefaultOptions(*dir).WithLogger(nil)
	if *mem {
		opt = badger.DefaultOptions("").WithInMemory(true).WithLogger(nil)
	}
	s, err := mustang.Open(opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "qt: %v\n", err)
		return 1
	}
	defer s.Close()

	r := &repl{s: s, tty: isatty.IsTerminal(os.Stdin.Fd()), out: os.Stdout}
	if r.tty {
		fmt.Fprintln(r.out, dimStyle.Render("mustang repl; type help, or quit"))
		return r.interactive()
	}
	return r.script(os.Stdin)
}

// interactive reads lines from the editor until quit or EOF.
func (r *repl) interactive() int {
	var hist []string
	for {
		line, err := readLine(r.prompt(), hist)
		if errors.Is(err, io.EOF) {
			line = "quit"
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "qt: %v\n", err)
			return 1
		}
		if len(strings.TrimSpace(line)) == 0 {
			continue
		}
		if len(hist) == 0 || hist[len(hist)-1] != line {
			hist = append(hist, line)
		}
		err = r.exec(line)
		if errors.Is(err, errQuit) {
			return 0
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, errStyle.Render(err.Error()))
		}
	}
}

// script runs one command per line; the first error ends it with status 1.
func (r *repl) script(in io.Reader) int {
	sc := bufio.NewScanner(in)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		err := r.exec(line)
		if errors.Is(err, errQuit) {
			return 0
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "qt: line %d: %v\n", n, err)
			return 1
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "qt: %v\n", err)
		return 1
	}
	if r.batch != nil {
		fmt.Fprintf(os.Stderr, "qt: batch of %d ops not committed, discarded\n", r.nops)
	}
	return 0
}

func (r *repl) prompt() string {
	if r.batch != nil {
		return fmt.Sprintf("batch %d: ", r.nops)
	}
	return ": "
}

func (r *repl) exec(line string) error {
	toks, err := tokenize(line)
	if err != nil {
		return err
	}
	if len(toks) == 0 {
		return nil
	}
	cmd, args := toks[0].s, toks[1:]
	switch cmd {
	case "help", "?":
		fmt.Fprint(r.out, usage)
		return nil
	case "quit", "exit", "q":
		return r.quit()
	case "tables":
		return r.list(r.s.ScanTables())
	case "rows":
		return r.withTable(args, func(t string) error { return r.list(r.s.ScanRows(t)) })
	case "cols":
		return r.withTable(args, func(t string) error { return r.list(r.s.ScanColumns(t)) })
	case "get":
		return r.get(args)
	case "column":
		return r.column(args)
	case "mget":
		return r.mget(args)
	case "set", "incr", "del", "dropcol", "drop", "if":
		return r.write(cmd, args)
	case "query", "count", "sum", "avg", "min", "max":
		return r.query(cmd, args)
	case "begin":
		if r.batch != nil {
			return errors.New("batch already open")
		}
		r.batch, r.nops = r.s.Batch(), 0
		return nil
	case "commit":
		if r.batch == nil {
			return errors.New("no open batch")
		}
		b := r.batch
		r.batch = nil
		return r.ok(b.Commit())
	case "abort":
		if r.batch == nil {
			return errors.New("no open batch")
		}
		r.batch = nil
		return nil
	}
	return fmt.Errorf("unknown command %q, see help", cmd)
}

// quit asks what to do with an open batch before leaving.
func (r *repl) quit() error {
	if r.batch == nil || !r.tty {
		return errQuit
	}
	var choice string
	err := huh.NewSelect[string]().
		Title(fmt.Sprintf("Batch of %d ops is not committed", r.nops)).
		Options(huh.NewOptions("commit", "discard", "cancel")...).
		Value(&choice).
		Run()
	if err != nil || choice == "cancel" {
		return err
	}
	if choice == "commit" {
		if err := r.batch.Commit(); err != nil {
			return err
		}
	}
	return errQuit
}

// withTable runs fn on the table named in args, or on one picked
// from a list when none is given on a terminal.
func (r *repl) withTable(args []token, fn func(t string) error) error {
	switch {
	case len(args) == 1:
		return fn(args[0].s)
	case len(args) > 1 || !r.tty:
		return errArgs
	}
	ts, err := r.s.ScanTables()
	if err != nil {
		return err
	}
	if len(ts) == 0 {
		return errors.New("no tables")
	}
	var t string
	err = huh.NewSelect[string]().
		Title("Table").
		Options(huh.NewOptions(ts...)...).
		Value(&t).
		Run()
	if err != nil {
		return err
	}
	return fn(t)
}

func (r *repl) get(args []token) error {
	switch len(args) {
	case 0, 1:
		return r.withTable(args, func(t string) error {
			tab, err := r.s.GetTable(t)
			if err != nil {
				return err
			}
			for _, k := range sortedKeys(tab) {
				r.row(k, tab[k])
			}
			return nil
		})
	case 2:
		row, err := r.s.GetRow(args[0].s, args[1].s)
		if err != nil {
			return err
		}
		r.row(args[1].s, row)
		return nil
	case 3:
		v, err := r.s.GetValue(args[0].s, args[1].s, args[2].s)
		if err != nil {
			return err
		}
		fmt.Fprintln(r.out, format(v))
		return nil
	}
	return errArgs
}

func (r *repl) column(args []token) error {
	if len(args) != 2 {
		return errArgs
	}
	col, err := r.s.GetColumn(args[0].s, args[1].s)
	if err != nil {
		return err
	}
	for _, k := range sortedKeys(col) {
		fmt.Fprintf(r.out, "%s  %s\n", keyStyle.Render(k), format(col[k]))
	}
	return nil
}

// mget prints one line per cell; a missing cell is shown as -.
func (r *repl) mget(args []token) error {
	if len(args) == 0 || len(args)%3 != 0 {
		return errArgs
	}
	cells := make([]mustang.Cell, 0, len(args)/3)
	for i := 0; i < len(args); i += 3 {
		cells = append(cells, mustang.Cell{Table: args[i].s, Row: args[i+1].s, Col: args[i+2].s})
	}
	vs, err := r.s.GetValues(cells)
	if err != nil {
		return err
	}
	for i, c := range cells {
		v := "-"
		if vs[i] != nil {
			v = format(vs[i])
		}
		fmt.Fprintf(r.out, "%s  %s\n", keyStyle.Render(c.Table+" "+c.Row+" "+c.Col), v)
	}
	return nil
}

// write adds a write to the open batch, or commits it on its own.
func (r *repl) write(cmd string, args []token) error {
	b := r.batch
	if b == nil {
		if cmd == "if" {
			return errors.New("if needs an open batch, see begin")
		}
		b = r.s.Batch()
	}

	n := len(args)
	switch {
	case cmd == "set" && n >= 4 && n%2 == 0:
		t, rk := args[0].s, args[1].s
		for i := 2; i < n; i += 2 {
			b.Set(t, rk, args[i].s, parse(args[i+1]))
		}
	case cmd == "incr" && (n == 3 || n == 4):
		d := 1.0
		if n == 4 {
			var err error
			if d, err = strconv.ParseFloat(args[3].s, 64); err != nil {
				return fmt.Errorf("bad delta %q", args[3].s)
			}
		}
		if r.batch == nil {
			v, err := r.s.Incr(args[0].s, args[1].s, args[2].s, d)
			if err != nil {
				return err
			}
			fmt.Fprintln(r.out, format(v))
			return nil
		}
		b.Incr(args[0].s, args[1].s, args[2].s, d)
	case cmd == "del" && n == 2:
		b.DeleteRow(args[0].s, args[1].s)
	case cmd == "del" && n == 3:
		b.Delete(args[0].s, args[1].s, args[2].s)
	case cmd == "dropcol" && n == 2:
		b.DeleteColumn(args[0].s, args[1].s)
	case cmd == "drop" && n <= 1:
		return r.withTable(args, func(t string) error {
			if !r.confirm(fmt.Sprintf("Delete table %s and all its rows?", t)) {
				return nil
			}
			b.DeleteTable(t)
			return r.finish(b)
		})
	case cmd == "if" && n >= 4:
		op, ok := ops[args[3].s]
		if !ok {
			return fmt.Errorf("unknown op %q", args[3].s)
		}
		var v mustang.Value
		if n == 5 {
			v = parse(args[4])
		} else if n > 5 || (op != mustang.Exists && op != mustang.Missing) {
			return errArgs
		}
		b.If(args[0].s, args[1].s, args[2].s, op, v)
	default:
		return errArgs
	}
	return r.finish(b)
}

// finish commits a one-off write, or counts it into the open batch.
func (r *repl) finish(b *mustang.Batch) error {
	if b != r.batch {
		return r.ok(b.Commit())
	}
	r.nops++
	return nil
}

func (r *repl) confirm(title string) bool {
	if !r.tty {
		return true
	}
	yes := false
	err := huh.NewConfirm().Title(title).Value(&yes).Run()
	return err == nil && yes
}

// query parses clauses after the table (and column, for aggregates).
func (r *repl) query(cmd string, args []token) error {
	agg := cmd != "query" && cmd != "count"
	need := 1
	if agg {
		need = 2
	}
	if len(args) < need {
		return errArgs
	}
	q := r.s.Query(args[0].s)
	col := args[need-1].s
	rest := args[need:]

	for len(rest) > 0 {
		kw := rest[0].s
		rest = rest[1:]
		if kw == "select" {
			if len(rest) == 0 {
				return errArgs
			}
			var cols []string
			for len(rest) > 0 && !isClause(rest[0].s) {
				cols = append(cols, rest[0].s)
				rest = rest[1:]
			}
			q.Select(cols...)
			continue
		}
		if kw == "reverse" {
			q.Reverse()
			continue
		}
		if kw == "where" {
			if len(rest) < 2 {
				return errArgs
			}
			op, ok := ops[rest[1].s]
			if !ok {
				return fmt.Errorf("unknown op %q", rest[1].s)
			}
			var v mustang.Value
			c := rest[0].s
			rest = rest[2:]
			if op != mustang.Exists && op != mustang.Missing {
				if len(rest) == 0 {
					return errArgs
				}
				v = parse(rest[0])
				rest = rest[1:]
			}
			q.Where(c, op, v)
			continue
		}
		if len(rest) == 0 {
			return errArgs
		}
		arg := rest[0].s
		rest = rest[1:]
		switch kw {
		case "prefix":
			q.Prefix(arg)
		case "from":
			q.From(arg)
		case "to":
			q.To(arg)
		case "limit":
			n, err := strconv.Atoi(arg)
			if err != nil || n < 0 {
				return fmt.Errorf("bad limit %q", arg)
			}
			q.Limit(n)
		default:
			return fmt.Errorf("unknown clause %q", kw)
		}
	}

	var (
		v   mustang.Value
		err error
	)
	switch cmd {
	case "query":
		res, err := q.Run()
		if err != nil {
			return err
		}
		for _, x := range res {
			r.row(x.Key, x.Row)
		}
		fmt.Fprintln(r.out, dimStyle.Render(fmt.Sprintf("%d rows", len(res))))
		return nil
	case "count":
		var n int
		n, err = q.Count()
		v = float64(n)
	case "sum":
		v, err = q.Sum(col)
	case "avg":
		v, err = q.Avg(col)
	case "min":
		v, err = q.Min(col)
	case "max":
		v, err = q.Max(col)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(r.out, format(v))
	return nil
}

func isClause(s string) bool {
	switch s {
	case "prefix", "from", "to", "limit", "reverse", "select", "where":
		return true
	}
	return false
}

func (r *repl) list(names []string, err error) error {
	if err != nil {
		return err
	}
	for _, n := range names {
		fmt.Fprintln(r.out, n)
	}
	return nil
}

func (r *repl) row(k string, row mustang.Row) {
	var b strings.Builder
	b.WriteString(keyStyle.Render(k))
	for _, c := range sortedKeys(row) {
		fmt.Fprintf(&b, "  %s=%s", c, format(row[c]))
	}
	fmt.Fprintln(r.out, b.String())
}

func (r *repl) ok(err error) error {
	if err != nil {
		return err
	}
	if r.tty {
		fmt.Fprintln(r.out, dimStyle.Render("ok"))
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// parse guesses the type of an unquoted word; see usage.
func parse(t token) mustang.Value {
	if t.quoted {
		return t.s
	}
	switch t.s {
	case "true":
		return true
	case "false":
		return false
	}
	// Only digit-led words are numbers, so "inf" and "nan" stay strings.
	if strings.ContainsAny(t.s[:1], "0123456789+-.") {
		if f, err := strconv.ParseFloat(t.s, 64); err == nil {
			return f
		}
	}
	return t.s
}

// format prints v so that parse reads it back as the same type.
func format(v mustang.Value) string {
	switch v := v.(type) {
	case string:
		return strconv.Quote(v)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return fmt.Sprint(v)
}

// tokenize splits on spaces; "double quotes" group words and take
// backslash escapes.
func tokenize(line string) ([]token, error) {
	var (
		toks []token
		cur  strings.Builder
		in   bool // inside a word
		q    bool // word is quoted
	)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			j := i + 1
			for ; j < len(line) && line[j] != '"'; j++ {
				if line[j] == '\\' {
					j++
				}
			}
			if j >= len(line) {
				return nil, errors.New("unterminated quote")
			}
			s, err := strconv.Unquote(line[i : j+1])
			if err != nil {
				return nil, fmt.Errorf("bad quoted string %s", line[i:j+1])
			}
			cur.WriteString(s)
			in, q = true, true
			i = j
		case c == ' ' || c == '\t':
			if in {
				toks = append(toks, token{cur.String(), q})
				cur.Reset()
				in, q = false, false
			}
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if in {
		toks = append(toks, token{cur.String(), q})
	}
	return toks, nil
}

// editor is a one-line input with history, run once per prompt.
type editor struct {
	in   textinput.Model
	hist []string
	pos  int // index into hist; len(hist) is the fresh line
	cur  string
	done bool
	eof  bool
}

func readLine(prompt string, hist []string) (string, error) {
	in := textinput.New()
	in.Prompt = prompt
	in.Focus()
	m, err := tea.NewProgram(editor{in: in, hist: hist, pos: len(hist)}).Run()
	if err != nil {
		return "", err
	}
	e := m.(editor)
	if e.eof {
		return "", io.EOF
	}
	return e.in.Value(), nil
}

func (e editor) Init() tea.Cmd {
	return textinput.Blink
}

func (e editor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		e.in, cmd = e.in.Update(msg)
		return e, cmd
	}
	switch k.Type {
	case tea.KeyEnter:
		e.done = true
		return e, tea.Quit
	case tea.KeyCtrlC:
		e.in.SetValue("")
		e.done = true
		return e, tea.Quit
	case tea.KeyCtrlD:
		if len(e.in.Value()) == 0 {
			e.done, e.eof = true, true
			return e, tea.Quit
		}
	case tea.KeyUp:
		if e.pos > 0 {
			if e.pos == len(e.hist) {
				e.cur = e.in.Value()
			}
			e.pos--
			e.in.SetValue(e.hist[e.pos])
			e.in.CursorEnd()
		}
		return e, nil
	case tea.KeyDown:
		if e.pos < len(e.hist) {
			e.pos++
			if e.pos == len(e.hist) {
				e.in.SetValue(e.cur)
			} else {
				e.in.SetValue(e.hist[e.pos])
			}
			e.in.CursorEnd()
		}
		return e, nil
	}
	var cmd tea.Cmd
	e.in, cmd = e.in.Update(msg)
	return e, cmd
}

// View leaves the plain line behind in the scrollback once done.
func (e editor) View() string {
	if e.done {
		return e.in.Prompt + e.in.Value() + "\n"
	}
	return e.in.View()
}
