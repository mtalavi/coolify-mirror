package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Rehost replaces host names in a text: a whole host name (not part of a
// longer name, not the domain of an e-mail address) and a domain written with
// a leading dot (a cookie domain, a *.wildcard). The same rules as
// cm_rehost in runner.php, which applies them to Coolify's own data.
func Rehost(text string, pairs map[string]string) string {
	if len(pairs) == 0 {
		return text
	}
	lp := make(map[string]string, len(pairs))
	olds := make([]string, 0, len(pairs))
	for o, n := range pairs {
		lp[strings.ToLower(o)] = n
		olds = append(olds, strings.ToLower(o))
	}
	pairs = lp
	sort.Slice(olds, func(i, j int) bool { return len(olds[i]) > len(olds[j]) })
	lower := strings.ToLower(text)
	var b strings.Builder
	for i := 0; i < len(text); {
		matched := false
		if i == 0 || !isHostByte(text[i-1]) && text[i-1] != '@' || dotDomainAt(text, i) {
			for _, o := range olds {
				if strings.HasPrefix(lower[i:], o) && (i+len(o) == len(text) || !isLabelByte(text[i+len(o)])) {
					b.WriteString(pairs[o])
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
// own folder are touched.
func rehostFiles(uuid string, files []string, pairs map[string]string) (changed []string, err error) {
	root := "/data/coolify/applications/" + uuid + "/"
	for _, f := range files {
		clean := filepath.Clean(f)
		if !strings.HasPrefix(clean, root) || clean != f {
			return changed, fmt.Errorf("refusing to change %s: outside %s", f, root)
		}
		data, err := os.ReadFile(clean)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return changed, err
		}
		out := Rehost(string(data), pairs)
		if out == string(data) {
			continue
		}
		if err := writeKeepingMode(clean, []byte(out)); err != nil {
			return changed, err
		}
		changed = append(changed, clean)
	}
	return changed, nil
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
