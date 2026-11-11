package importer

import (
	"fmt"
	"strings"
	"unicode"
)

type tokKind int

const (
	tIdent tokKind = iota
	tString
	tNumber
	tOp
	tEOF
)

type token struct {
	kind tokKind
	val  string
}

func lex(s string) ([]token, error) {
	var out []token
	for i := 0; i < len(s); {
		ch := rune(s[i])
		switch {
		case unicode.IsSpace(ch):
			i++
		case ch == '\'' || ch == '"':
			q := s[i]
			j := i + 1
			var b strings.Builder
			for j < len(s) && s[j] != q {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				b.WriteByte(s[j])
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated string")
			}
			out = append(out, token{tString, b.String()})
			i = j + 1
		case unicode.IsLetter(ch) || ch == '_':
			j := i
			for j < len(s) && (unicode.IsLetter(rune(s[j])) || unicode.IsDigit(rune(s[j])) || s[j] == '_') {
				j++
			}
			out = append(out, token{tIdent, s[i:j]})
			i = j
		case unicode.IsDigit(ch):
			j := i
			for j < len(s) && (unicode.IsDigit(rune(s[j])) || s[j] == '.') {
				j++
			}
			out = append(out, token{tNumber, s[i:j]})
			i = j
		default:
			two := ""
			if i+1 < len(s) {
				two = s[i : i+2]
			}
			switch two {
			case "==", "!=", "&&", "||":
				out = append(out, token{tOp, two})
				i += 2
				continue
			}
			switch ch {
			case '!', '(', ')', '+', ',':
				out = append(out, token{tOp, string(ch)})
				i++
			default:
				return nil, fmt.Errorf("unsupported character %q", ch)
			}
		}
	}
	return append(out, token{tEOF, ""}), nil
}

type exprParser struct {
	toks []token
	pos  int

	vars map[string]bool
}

func (p *exprParser) peek() token { return p.toks[p.pos] }
func (p *exprParser) next() token { t := p.toks[p.pos]; p.pos++; return t }
func (p *exprParser) accept(op string) bool {
	if t := p.peek(); t.kind == tOp && t.val == op {
		p.pos++
		return true
	}
	return false
}

type value struct {
	word    string
	cond    string
	isWord  bool
	literal bool
	ident   string
}

func (p *exprParser) or() (value, error) {
	l, err := p.and()
	if err != nil {
		return l, err
	}
	for p.accept("||") {
		r, err := p.and()
		if err != nil {
			return r, err
		}
		l = foldOr(l, r)
	}
	return l, nil
}

func (p *exprParser) and() (value, error) {
	l, err := p.unary()
	if err != nil {
		return l, err
	}
	for p.accept("&&") {
		r, err := p.unary()
		if err != nil {
			return r, err
		}
		l = foldAnd(l, r)
	}
	return l, nil
}

func (p *exprParser) unary() (value, error) {
	if p.accept("!") {
		v, err := p.unary()
		if err != nil {
			return v, err
		}
		switch c := asCond(v); c {
		case "true":
			return value{cond: "false"}, nil
		case "false":
			return value{cond: "true"}, nil
		default:
			return value{cond: "! " + c}, nil
		}
	}
	return p.cmp()
}

func (p *exprParser) cmp() (value, error) {
	l, err := p.sum()
	if err != nil {
		return l, err
	}
	for _, op := range []string{"==", "!="} {
		if p.accept(op) {
			r, err := p.sum()
			if err != nil {
				return r, err
			}
			if !l.isWord || !r.isWord {
				return l, fmt.Errorf("cannot compare conditions")
			}
			if l.literal && r.literal {
				if (l.word == r.word) == (op == "==") {
					return value{cond: "true"}, nil
				}
				return value{cond: "false"}, nil
			}
			return value{cond: fmt.Sprintf(`[[ "%s" %s "%s" ]]`, l.word, op, r.word)}, nil
		}
	}
	return l, nil
}

func (p *exprParser) sum() (value, error) {
	l, err := p.primary()
	if err != nil {
		return l, err
	}
	for p.accept("+") {
		r, err := p.primary()
		if err != nil {
			return r, err
		}
		if !l.isWord || !r.isWord {
			return l, fmt.Errorf("cannot concatenate conditions")
		}
		l = value{isWord: true, word: l.word + r.word, literal: l.literal && r.literal}
	}
	return l, nil
}

func (p *exprParser) primary() (value, error) {
	t := p.next()
	switch t.kind {
	case tString, tNumber:
		return value{isWord: true, literal: true, word: dqEscape(t.val)}, nil
	case tOp:
		if t.val == "(" {
			v, err := p.or()
			if err != nil {
				return v, err
			}
			if !p.accept(")") {
				return v, fmt.Errorf("missing )")
			}
			if v.isWord || v.cond == "true" || v.cond == "false" {
				return v, nil
			}
			return value{cond: "( " + v.cond + " )"}, nil
		}
	case tIdent:
		if p.accept("(") {
			var args []value
			if !p.accept(")") {
				for {
					a, err := p.or()
					if err != nil {
						return a, err
					}
					args = append(args, a)
					if p.accept(")") {
						break
					}
					if !p.accept(",") {
						return a, fmt.Errorf("expected , or )")
					}
				}
			}
			switch t.val {
			case "file_exists":
				if len(args) != 1 || !args[0].isWord {
					return value{}, fmt.Errorf("file_exists takes one path")
				}
				return value{cond: fmt.Sprintf(`[[ -e "%s" ]]`, args[0].word)}, nil
			}
			return value{}, fmt.Errorf("function %s is not supported", t.val)
		}
		switch t.val {
		case "true":
			return value{cond: "true"}, nil
		case "false":
			return value{cond: "false"}, nil
		case "env":
			return value{isWord: true, literal: true, word: "host"}, nil
		case "rootDir":
			return value{isWord: true, word: "${SERVER_DIR}"}, nil
		}
		if !validKey(t.val) {
			return value{}, fmt.Errorf("unsupported name %q", t.val)
		}

		return value{isWord: true, word: "${" + t.val + "}", ident: t.val}, nil
	}
	return value{}, fmt.Errorf("unexpected %q", t.val)
}

func asCond(v value) string {
	if !v.isWord {
		return v.cond
	}
	return fmt.Sprintf(`[[ "%s" == "true" || "%s" == "1" ]]`, v.word, v.word)
}

func bashCondition(expr string, vars map[string]bool) (string, error) {
	toks, err := lex(expr)
	if err != nil {
		return "", err
	}
	p := &exprParser{toks: toks, vars: vars}
	v, err := p.or()
	if err != nil {
		return "", err
	}
	if p.peek().kind != tEOF {
		return "", fmt.Errorf("unexpected %q", p.peek().val)
	}
	return asCond(v), nil
}

func dqEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return r.Replace(s)
}

func bashWord(s string, glob bool) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "${") {
			if end := strings.IndexByte(s[i:], '}'); end > 2 {
				name := s[i+2 : i+end]
				switch {
				case name == "rootDir":
					b.WriteString("${SERVER_DIR}")
					i += end + 1
					continue
				case validKey(name):
					b.WriteString("${" + name + "}")
					i += end + 1
					continue
				}
			}
		}
		if glob && s[i] == '*' {
			b.WriteString(`"*"`)
			i++
			continue
		}
		b.WriteString(dqEscape(s[i : i+1]))
		i++
	}
	b.WriteByte('"')
	return b.String()
}

func foldAnd(l, r value) value {
	a, b := asCond(l), asCond(r)
	switch {
	case a == "false" || b == "false":
		return value{cond: "false"}
	case a == "true":
		return value{cond: b}
	case b == "true":
		return value{cond: a}
	}
	return value{cond: "{ " + a + " && " + b + "; }"}
}

func foldOr(l, r value) value {
	a, b := asCond(l), asCond(r)
	switch {
	case a == "true" || b == "true":
		return value{cond: "true"}
	case a == "false":
		return value{cond: b}
	case b == "false":
		return value{cond: a}
	}
	return value{cond: "{ " + a + " || " + b + "; }"}
}
