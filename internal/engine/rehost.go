package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Rehost replaces host names in a text: a whole host name (not part of a
// longer name, not the domain of an e-mail address), a domain written with a
// leading dot (a cookie domain, a *.wildcard) and a host name written as a
// regular expression (Traefik HostRegexp: vemela[.]app, vemela\.app or
// vemela\\.app). The same rules as cm_rehost in runner.php, which applies
// them to Coolify's own data.
func Rehost(text string, pairs map[string]string) string {
	out, _ := RehostWildcards(text, pairs)
	return out
}

// regexDots are the spellings of a literal dot in a regular expression, the
// longest first: [.], \\. (inside a double-quoted YAML string) and \.
var regexDots = []string{"[.]", `\\.`, `\.`}

// RehostWildcards is Rehost that also returns the new names that now stand
// for every subdomain ("*.ir.example.com"): a replaced *.wildcard or a
// replaced host name in a pattern that matches names in front of it, such as
// ^[a-z0-9-]+[.]example[.]com$. Such names need a DNS record and a
// certificate of their own for every subdomain.
func RehostWildcards(text string, pairs map[string]string) (string, []string) {
	if len(pairs) == 0 {
		return text, nil
	}
	lp := make(map[string]string, len(pairs))
	olds := make([]string, 0, len(pairs))
	for o, n := range pairs {
		lp[strings.ToLower(o)] = n
		olds = append(olds, strings.ToLower(o))
	}
	pairs = lp
	sort.Slice(olds, func(i, j int) bool { return len(olds[i]) > len(olds[j]) })
	wild := map[string]bool{}
	lower := strings.ToLower(text)
	var b strings.Builder
	for i := 0; i < len(text); {
		matched := false
		if i == 0 || !isHostByte(text[i-1]) && text[i-1] != '@' || dotDomainAt(text, i) {
			for _, o := range olds {
				if strings.HasPrefix(lower[i:], o) && (i+len(o) == len(text) || !isLabelByte(text[i+len(o)])) {
					b.WriteString(pairs[o])
					if i >= 2 && text[i-1] == '.' && text[i-2] == '*' {
						wild[strings.ToLower(pairs[o])] = true
					}
					i += len(o)
					matched = true
					break
				}
			}
		}
		if !matched {
			b.WriteByte(text[i])
			i++
		}
	}
	text = b.String()
	for _, dot := range regexDots {
		text = rehostRegex(text, pairs, olds, dot, wild)
	}
	out := make([]string, 0, len(wild))
	for h := range wild {
		out = append(out, "*."+h)
	}
	sort.Strings(out)
	return text, out
}

// rehostRegex replaces the host names written with dot as the dot. A name
// right after a label and a dot (shop[.]example[.]com) is the subdomain of
// another name and stays; so does a name that is already the new one
// (ir[.]example[.]com), which keeps the replacement idempotent.
func rehostRegex(text string, pairs map[string]string, olds []string, dot string, wild map[string]bool) string {
	if !strings.Contains(text, dot) {
		return text
	}
	type form struct{ old, new, host string }
	forms := make([]form, 0, len(olds))
	for _, o := range olds {
		if !strings.Contains(o, ".") {
			continue
		}
		forms = append(forms, form{strings.ReplaceAll(o, ".", dot), strings.ReplaceAll(pairs[o], ".", dot), strings.ToLower(pairs[o])})
	}
	lower := strings.ToLower(text)
	var b strings.Builder
	for i := 0; i < len(text); {
		matched := false
		if i == 0 || !isLabelByte(text[i-1]) {
			for _, f := range forms {
				if !strings.HasPrefix(lower[i:], f.old) || i+len(f.old) < len(text) && isLabelByte(text[i+len(f.old)]) {
					continue
				}
				before := text[:i]
				afterDot := strings.HasSuffix(before, dot)
				if afterDot && len(before) > len(dot) && isLabelByte(before[len(before)-len(dot)-1]) {
					break // the subdomain of another name
				}
				b.WriteString(f.new)
				if afterDot || strings.HasSuffix(before, ")?") || strings.HasSuffix(before, ")*") {
					wild[f.host] = true
				}
				i += len(f.old)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(text[i])
			i++
		}
	}
	return b.String()
}

// dotDomainAt: the host at i follows a dot that starts a domain (".example.com"
// after "=", "*", a space, a comma or a quote).
func dotDomainAt(text string, i int) bool {
	if i < 2 || text[i-1] != '.' {
		return false
	}
	return strings.IndexByte("=* \t\n\r\f\v,\"'", text[i-2]) >= 0
}

func isLabelByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

func isHostByte(c byte) bool { return isLabelByte(c) || c == '.' }

// rehostFiles applies Rehost to env files of an application (paths given by
// Coolify), keeping their mode and owner. Only files inside the application's
// own folder are touched. wild: the names that now stand for every subdomain.
func rehostFiles(uuid string, files []string, pairs map[string]string) (changed, wild []string, err error) {
	root := "/data/coolify/applications/" + uuid + "/"
	for _, f := range files {
		clean := filepath.Clean(f)
		if !strings.HasPrefix(clean, root) || clean != f {
			return changed, wild, fmt.Errorf("refusing to change %s: outside %s", f, root)
		}
		data, err := os.ReadFile(clean)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return changed, wild, err
		}
		out, w := RehostWildcards(string(data), pairs)
		if out == string(data) {
			continue
		}
		if err := writeKeepingMode(clean, []byte(out)); err != nil {
			return changed, wild, err
		}
		changed = append(changed, clean)
		wild = append(wild, w...)
	}
	return changed, wild, nil
}

// interpolate resolves ${VAR}, ${VAR:-default}, ${VAR-default}, ${VAR:?err},
// ${VAR?err}, $VAR and $$ the way Docker Compose does, from env.
func interpolate(s string, env map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '$' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		n := s[i+1]
		switch {
		case n == '$':
			b.WriteByte('$')
			i++
		case n == '{':
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				b.WriteString(s[i:])
				return b.String()
			}
			expr := s[i+2 : i+2+end]
			b.WriteString(resolveVar(expr, env))
			i += 2 + end
		case isLabelByte(n) && n != '-':
			j := i + 1
			for j < len(s) && (isLabelByte(s[j]) && s[j] != '-') {
				j++
			}
			b.WriteString(env[s[i+1:j]])
			i = j - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func resolveVar(expr string, env map[string]string) string {
	for _, op := range []string{":-", ":?", "-", "?"} {
		if k := strings.Index(expr, op); k > 0 {
			name, arg := expr[:k], expr[k+len(op):]
			v, set := env[name]
			empty := !set || (strings.HasPrefix(op, ":") && v == "")
			switch {
			case !empty:
				return v
			case strings.HasSuffix(op, "-"):
				return arg
			default:
				return ""
			}
		}
	}
	return env[expr]
}

// parseDotEnv reads KEY=VALUE lines as Coolify writes them (values may be
// quoted).
func parseDotEnv(text string) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		env[k] = v
	}
	return env
}
