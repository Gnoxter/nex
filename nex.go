// Substantial copy-and-paste from src/pkg/regexp.
package main
import ("bufio";"flag";"fmt";"io";"os";"strings")
type rule struct {
  regex []int
  code string
  id, family, index int
}
type Error string

func (e Error) String() string { return string(e) }

var (
  ErrInternal            = Error("internal error")
  ErrUnmatchedLpar       = Error("unmatched '('")
  ErrUnmatchedRpar       = Error("unmatched ')'")
  ErrUnmatchedLbkt       = Error("unmatched '['")
  ErrUnmatchedRbkt       = Error("unmatched ']'")
  ErrBadRange            = Error("bad range in character class")
  ErrExtraneousBackslash = Error("extraneous backslash")
  ErrBareClosure         = Error("closure applies to nothing")
  ErrBadBackslash        = Error("illegal backslash escape")
)
func ispunct(c int) bool {
  for _, r := range "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~" {
    if c == r { return true }
  }
  return false
}
var escapes = []int("abfnrtv")
var escaped = []int("\a\b\f\n\r\t\v")
func escape(c int) int {
  for i, b := range escapes {
    if int(b) == c { return i }
  }
  return -1
}
type edge struct {
  // TODO: Make kind an enum (and use iota)
  // 2 = rune class edge
  // 1 = rune edge
  // 0 = empty edge
  // -1 = wild edge (".")
  kind int
  r int        // Rune; for rune edges.
  lim []int    // Pairs of limits for character class edges.
  negate bool  // True if the character class is negated.
  dst *node    // Destination node.
}
type node struct {
  e []*edge    // Slice of outedges.
  n int        // Index number. Scoped to a family.
  accept bool  // True if this is an accepting state.
  set []int    // The NFA nodes represented by a DFA node.
}
func inClass(r int, lim []int) bool {
  for i := 0; i < len(lim); i+=2 {
    if lim[i] <= r && r <= lim[i+1] { return true }
  }
  return false
}
func gen(out io.Writer, x *rule) {
  // End rule
  if -1 == x.index {
    fmt.Fprintf(out, "a[%d].endcase = %d\n", x.family, x.id);
    return
  }
  s := x.regex
  // Regex -> NFA
  alph := make(map[int]bool)
  lim := make([]int, 0, 8)
  pos := 0
  n := 0
  newNode := func() *node {
    res := new(node)
    res.n = n
    res.e = make([]*edge, 0, 8)
    n++
    return res
  }
  newEdge := func(u, v *node) *edge {
    res := new(edge)
    res.dst = v
    u.e = append(u.e, res)
    return res
  }
  newWildEdge := func(u, v *node) *edge {
    res := newEdge(u, v)
    res.kind = 1
    return res
  }
  newRuneEdge := func(u, v *node, r int) *edge {
    res := newEdge(u, v)
    res.kind = 0
    res.r = r
    alph[r] = true
    return res
  }
  newNilEdge := func(u, v *node) *edge {
    res := newEdge(u, v)
    res.kind = -1
    return res
  }
  newClassEdge := func(u, v *node) *edge {
    res := newEdge(u, v)
    res.kind = 2
    res.lim = make([]int, 0, 2)
    return res
  }
  nlpar := 0
  maybeEscape := func() int {
    c := s[pos]
    if '\\' == c {
      pos++
      if len(s) == pos { panic(ErrExtraneousBackslash) }
      c = s[pos]
      switch {
      case ispunct(c):
      case escape(c) >= 0: c = escaped[escape(s[pos])]
      default: panic(ErrBadBackslash)
      }
    }
    return c
  }
  pcharclass := func() (start, end *node) {
    start = newNode()
    end = newNode()
    e := newClassEdge(start, end)
    if len(s) > pos && '^' == s[pos] {
      e.negate = true
      pos++
    }
    left := -1
    for {
      if len(s) == pos || s[pos] == ']' {
	if left >= 0 { panic(ErrBadRange) }
	return
      }
      switch(s[pos]) {
      case '-':
        panic(ErrBadRange)
      default:
        c := maybeEscape()
	pos++
	if len(s) == pos { panic(ErrBadRange) }
	switch {
	case left < 0:  // Lower limit.
	  if '-' == s[pos] {
	    pos++
	    left = c
	  } else {
	    e.lim = append(e.lim, c, c)
	    alph[c] = true
	  }
	case left <= c: // Upper limit.
	  e.lim = append(e.lim, left, c)
	  if left == c {
	    alph[c] = true
	  } else {
	    lim = append(lim, left, c)
	  }
	  left = -1
	default:
	  panic(ErrBadRange)
	}
      }
    }
    panic("unreachable")
  }
  var pre func() (start, end *node)
  pterm := func() (start, end *node) {
    if len(s) == pos || s[pos] == '|' {
      end = newNode()
      start = end
      return
    }
    switch s[pos] {
    case '*','+','?': panic(ErrBareClosure)
    case ')':
      if 0 == nlpar { panic(ErrUnmatchedRpar) }
      end = newNode()
      start = end
      return
    case '(':
      nlpar++
      pos++
      start, end = pre()
      if len(s) == pos || ')' != s[pos] { panic(ErrUnmatchedLpar) }
    case '.':
      start = newNode()
      end = newNode()
      newWildEdge(start, end)
    case ']':
      panic(ErrUnmatchedRbkt)
    case '[':
      pos++
      start, end = pcharclass()
      if len(s) == pos || ']' != s[pos] { panic(ErrUnmatchedLbkt) }
    default:
      start = newNode()
      end = newNode()
      newRuneEdge(start, end, maybeEscape())
    }
    pos++
    return
  }
  pclosure := func() (start, end *node) {
    start, end = pterm()
    if start == end { return }
    if len(s) == pos { return }
    switch(s[pos]) {
    case '*':
      newNilEdge(end, start)
      nend := newNode()
      newNilEdge(end, nend)
      start = end
      end = nend
    case '+':
      newNilEdge(end, start)
      nend := newNode()
      newNilEdge(end, nend)
      end = nend
    case '?':
      newNilEdge(start, end)
    default:
      return
    }
    pos++
    return
  }
  pcat := func() (start, end *node) {
    for {
      nstart, nend := pclosure()
      if start == nil { start, end = nstart, nend } else if nstart != nend {
	end.e = make([]*edge, len(nstart.e))
	copy(end.e, nstart.e)
	end = nend
      }
      if nstart == nend { return }
    }
    panic("unreachable")
  }
  pre = func() (start, end *node) {
    start,end = pcat()
    for {
      if len(s) == pos { return }
      if s[pos] != '|' { return }
      pos++;
      nstart,nend := pcat()
      tmp := newNode()
      newNilEdge(tmp, start)
      newNilEdge(tmp, nstart)
      start = tmp
      tmp = newNode()
      newNilEdge(end, tmp)
      newNilEdge(nend, tmp)
      end = tmp
    }
    panic("unreachable")
  }
  start, end := pre()
  end.accept = true

  // Compute shortlist of nodes, as we may have discarded nodes left over
  // from parsing. Also, make short[0] the start node.
  short := make([]*node, 0, n)
  {
    var visit func(*node)
    mark := make([]bool, n)
    newn := make([]int, n)
    visit = func(u *node) {
      mark[u.n] = true
      newn[u.n] = len(short)
      short = append(short, u)
      for _,e := range u.e {
	if !mark[e.dst.n] {
	  visit(e.dst)
	}
      }
    }
    visit(start)
    for _,v := range short {
      v.n = newn[v.n]
    }
  }
  n = len(short)

/*
  {
  var show func(*node)
  mark := make([]bool, n)
  show = func(u *node) {
    mark[u.n] = true
    print(u.n, ": ")
    for _,e := range u.e {
      print("(", e.kind, " ", e.r, ")")
      print(e.dst.n)
    }
    println()
    for _,e := range u.e {
      if !mark[e.dst.n] {
	show(e.dst)
      }
    }
  }
  show(start)
  }
  */

  // NFA -> DFA
  nilClose := func(st []bool) {
    mark := make([]bool, n)
    var do func(int)
    do = func(i int) {
      v := short[i]
      for _,e := range v.e {
	if -1 == e.kind && !mark[e.dst.n] {
	  st[e.dst.n] = true
	  do(e.dst.n)
	}
      }
    }
    for i := 0; i < n; i++ {
      if st[i] && !mark[i] {
	mark[i] = true
	do(i)
      }
    }
  }
  todo := make([]*node, 0, n)
  tab := make(map[string]*node)
  buf := make([]byte, 0, 8)
  dfacount := 0
  {
    for i := 0; i < n; i++ {
      buf = append(buf, '0')
    }
    tmp := new(node)
    tmp.n = -1
    tab[string(buf)] = tmp
  }
  newDFANode := func(st []bool) (res *node, found bool) {
    buf = buf[:0]
    accept := false
    for i,v := range st {
      if v {
	buf = append(buf, '1')
	accept = accept || short[i].accept
      } else { buf = append(buf, '0') }
    }
    res, found = tab[string(buf)]
    if !found {
      res = new(node)
      res.set = make([]int, 0, 8)
      res.n = dfacount
      res.accept = accept
      dfacount++
      for i,v := range st {
	if v { res.set = append(res.set, i) }
      }
      tab[string(buf)] = res
    }
    return res, found
  }

  get := func(states []bool) *node {
    nilClose(states)
    node, old := newDFANode(states)
    if !old {
      todo = append(todo, node)
    }
    return node
  }
  states := make([]bool, n)
  states[0] = true
  get(states)
  for len(todo) > 0 {
    v := todo[len(todo)-1]
    todo = todo[0:len(todo)-1]
    // Singles.
    for r,_ := range alph {
      states := make([]bool, n)
      for _,i := range v.set {
	for _,e := range short[i].e {
	  if e.kind == 0 && e.r == r {
	    states[e.dst.n] = true
	  } else if e.kind == 1 {
	    states[e.dst.n] = true
	  } else if e.kind == 2 && e.negate != inClass(r, e.lim) {
	    states[e.dst.n] = true
	  }
	}
      }
      newRuneEdge(v, get(states), r)
    }
    // Character ranges.
    for j := 0; j < len(lim); j+=2 {
      states := make([]bool, n)
      for _,i := range v.set {
	for _,e := range short[i].e {
	  if e.kind == 1 {
	    states[e.dst.n] = true
	  } else if e.kind == 2 && e.negate != inClass(lim[j], e.lim) {
	    states[e.dst.n] = true
	  }
	}
      }
      e := newClassEdge(v, get(states))
      e.lim = append(e.lim, lim[j], lim[j+1])
    }
    // Other.
    states := make([]bool, n)
    for _,i := range v.set {
      for _,e := range short[i].e {
	if e.kind == 1 || (e.kind == 2 && e.negate) {
	  states[e.dst.n] = true
	}
      }
    }
    newWildEdge(v, get(states))
  }
  n = dfacount

  // DFA -> Go
  // TODO: Literal arrays instead of a series of assignments.
  fmt.Fprintf(out, "{\nvar acc [%d]bool\nvar fun [%d]func(int) int\n", n, n)
  for _,v := range tab {
    if -1 == v.n { continue }
    if v.accept {
      fmt.Fprintf(out, "acc[%d] = true\n", v.n)
    }
    fmt.Fprintf(out, "fun[%d] = func(r int) int {\n", v.n)
    fmt.Fprintf(out, "  switch(r) {\n")
    for _,e := range v.e {
      m := e.dst.n
      if e.kind == 0 {
	fmt.Fprintf(out, "  case %d: return %d\n", e.r, m)
      }
    }
    fmt.Fprintf(out, "  default:\n    switch {\n")
    for _,e := range v.e {
      m := e.dst.n
      if e.kind == 2 {
	fmt.Fprintf(out, "    %d <= r && r <= %d: return %d\n",
	    e.lim[0], e.lim[1], m)
      } else if e.kind == 1 {
	fmt.Fprintf(out, "    default: return %d\n", m)
      }
    }
    fmt.Fprintf(out, "    }\n  }\n  panic(\"unreachable\")\n}\n")
  }
  fmt.Fprintf(out, "a%d[%d].acc = acc[:]\n", x.family, x.index);
  fmt.Fprintf(out, "a%d[%d].f = fun[:]\n", x.family, x.index);
  fmt.Fprintf(out, "a%d[%d].id = %d\n", x.family, x.index, x.id);
  fmt.Fprintf(out, "}\n")
}
func writeActions(out *bufio.Writer, rules []*rule, prefix string) {
  fmt.Fprintf(out, `func(nnCtx interface {}) {
  for nnDone := false; !nnDone; {
    switch %s.Next(nnCtx) {`, prefix)
  for _, x := range rules {
    fmt.Fprintf(out, "\n    case %d:  //%s/\n", x.id, string(x.regex))
    out.WriteString(x.code)
  }
  out.WriteString("    }\n  }\n}")
}
func process(in *bufio.Reader, out, outmain *bufio.Writer) {
  var r int
  done := false
  regex := make([]int, 0, 8)
  read := func() {
    var er os.Error
    r,_,er = in.ReadRune()
    if er == os.EOF { done = true } else if er != nil { panic(er.String()) }
  }
  skipws := func() {
    for {
      read()
      if done { break }
      if strings.IndexRune(" \n\t\r", r) == -1 { break }
    }
  }
  var rules []*rule
  usercode := false
  familyn := 1
  id := 0
  newRule := func(family, index int) *rule {
    x := new(rule)
    rules = append(rules, x)
    x.family = family
    x.id = id
    x.index = index
    id++
    return x
  }
  buf := make([]int, 0, 8)
  readCode := func() string {
    if '{' != r { panic("expected {") }
    buf = buf[:0]
    for {
      buf = append(buf, r)
      read()
      if done { panic("unmatched {") }
      if '}' == r { break }
    }
    buf = append(buf, r)
    return string(buf)
  }
  readWord := func() string {
    buf = buf[:0]
    for {
      buf = append(buf, r)
      read()
      if done || strings.IndexRune(" \n\t\r", r) != -1 { break }
    }
    return string(buf)
  }
  skipws()
  if done || "package" != readWord() { panic("expected package declaration") }
  skipws()
  if done { panic("expected package name") }
  name := readWord()
  var parse func(int)
  parse = func(family int) {
    rulen := 0
    declvar := func() { fmt.Fprintf(out, "var a%d [%d]dfa\n", family, rulen) }
    for !done {
      skipws()
      if done { break }
      regex = regex[:0]
      if '>' == r {
	if 0 == family { panic("unmatched >") }
	x := newRule(family, -1)
	x.code = fmt.Sprintf("nnCtx = %s.Pop(nnCtx)\n", name)
	declvar()
	skipws()
	x.code += readCode()
	return
      }
      delim := r
      read()
      if done { panic("unterminated pattern") }
      for {
	if r == delim && (len(regex) == 0 || regex[len(regex) - 1] != '\\') {
	  break
	}
	if '\n' == r { panic("regex interrupted by newline") }
	regex = append(regex, r)
	read()
	if done { panic("unterminated pattern") }
      }
      if "" == string(regex) {
	usercode = true
	break
      }
      skipws()
      if done { panic("last pattern lacks action") }
      x := newRule(family, rulen)
      rulen++
      x.regex = make([]int, len(regex))
      copy(x.regex, regex)
      nested := false
      if '<' == r {
	skipws()
	if done { panic("'<' lacks action") }
	x.code = fmt.Sprintf("nnCtx = %s.Push(nnCtx, %d)\n", name, familyn)
	nested = true
      }
      x.code += readCode()
      if nested {
	familyn++
	parse(familyn-1)
      }
    }
    if 0 != family { panic("unmatched <") }
    x := newRule(family, -1)
    x.code = "nnDone = true\n"
    declvar()
  }
  fmt.Fprintf(out, "package %s\n", name)
  fmt.Fprintf(out, `import ("bufio";"os";"strings")
type dfa struct {
  acc []bool
  f []func(int) int
  id int
}
type family struct {
  a []dfa
  endcase int
}
`)

  parse(0)

  out.WriteString("var a []family\n")
  out.WriteString("func init() {\n")

  fmt.Fprintf(out, "a = make([]family, %d)\n", familyn)
  for _, x := range rules { gen(out, x) }
  for i := 0; i < familyn; i++ {
    fmt.Fprintf(out, "a[%d].a = a%d[:]\n", i, i)
  }

  out.WriteString(`}
func getAction(c *frame) int {
  if -1 == c.match { panic("No match") }
  c.action = c.fam.a[c.match].id
  c.match = -1
  return c.action
}
type frame struct {
  atEOF bool
  action, match, matchn, n int
  buf []int
  text string
  in *bufio.Reader
  state []int
  fam family
}
func newFrame(in *bufio.Reader, index int) *frame {
  f := new(frame)
  f.buf = make([]int, 0, 128)
  f.in = in
  f.match = -1
  f.fam = a[index]
  f.state = make([]int, len(f.fam.a))
  return f
}
func Start(in *bufio.Reader) interface{} {
  stack := make([]*frame, 0, 4)
  stack = append(stack, newFrame(in, 0))
  return stack
}
func Next(p interface {}) int {
  stack := p.([]*frame)
  c := stack[len(stack) - 1]
  for {
    if c.atEOF { return c.fam.endcase }
    if c.n == len(c.buf) {
      r,_,er := c.in.ReadRune()
      switch er {
      case nil: c.buf = append(c.buf, r)
      case os.EOF:
	c.atEOF = true
	if c.n > 0 {
	  c.text = string(c.buf)
	  return getAction(c)
	}
	return c.fam.endcase
      default: panic(er.String())
      }
    }
    jammed := true
    r := c.buf[c.n]
    for i, x := range c.fam.a {
      if -1 == c.state[i] { continue }
      c.state[i] = x.f[c.state[i]](r)
      if -1 == c.state[i] { continue }
      jammed = false
      if x.acc[c.state[i]] {
	if -1 == c.match || c.matchn < c.n+1 || c.match > i {
	  c.match = i
	  c.matchn = c.n+1
	}
      }
    }
    if jammed {
      c.n = 0
      for i, _ := range c.state { c.state[i] = 0 }
      c.text = string(c.buf[:c.matchn])
      copy(c.buf, c.buf[c.matchn:])
      c.buf = c.buf[:len(c.buf) - c.matchn]
      return getAction(c)
    }
    c.n++
  }
  panic("unreachable")
}
func Push(p interface {}, index int) interface {} {
  stack := p.([]*frame)
  c := stack[len(stack) - 1]
  return append(stack,
      newFrame(bufio.NewReader(strings.NewReader(c.text)), index))
}
func Pop(p interface {}) interface {} {
  stack := p.([]*frame)
  return stack[:len(stack) - 1]
}
func Text(p interface {}) string {
  stack := p.([]*frame)
  c := stack[len(stack) - 1]
  return c.text
}
`)
  out.Flush()

  if !usercode { return }
  skipws()
  buf = buf[:0]
  const macro = "NN_FUN"
  for !done {
    buf = append(buf, r)
    if macro[0:len(buf)] != string(buf) {
      outmain.WriteString(string(buf))
      buf = buf[:0]
    } else if len(macro) == len(buf) {
      writeActions(outmain, rules, name)
      buf = buf[:0]
    }
    read()
  }
  outmain.WriteString(string(buf))
  outmain.Flush()
}

func main() {
  run := func(name string, file *os.File) {
    f,er := os.Open(name + ".go", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
    if f == nil {
      println("nex:", er.String())
      os.Exit(1)
    }
    outmain := bufio.NewWriter(os.Stdout)
    out := bufio.NewWriter(f)
    process(bufio.NewReader(file), out, outmain)
    f.Close()
  }
  flag.Parse()
  if 0 == flag.NArg() {
    run("_nn", os.Stdin)
    return
  }
  if flag.NArg() > 1 {
    println("nex: extraneous arguments after", flag.Arg(1))
    os.Exit(1)
  }
  if strings.HasSuffix(flag.Arg(1), ".go") {
    println("nex: input filename ends with .go:", flag.Arg(1))
    os.Exit(1)
  }
  basename := flag.Arg(0)
  n := strings.LastIndex(basename, ".")
  if n >= 0 { basename = basename[:n] }
  name := basename + ".nn"
  f,er := os.Open(flag.Arg(0), os.O_RDONLY, 0)
  if f == nil {
    println("nex:", er.String())
    os.Exit(1)
  }
  run(name, f)
  f.Close()
}
