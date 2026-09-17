package graph

import (
	"strconv"
	"strings"
)

// CompareVersions orders version strings. Semantic versions (with an optional
// leading "v", as Go uses) are compared by semver precedence, including
// pre-release identifiers. Anything else falls back to a natural ordering
// where digit runs compare numerically.
func CompareVersions(a, b string) int {
	if sa, ok := parseSemver(a); ok {
		if sb, ok := parseSemver(b); ok {
			return compareSemver(sa, sb)
		}
	}
	return naturalCompare(a, b)
}

type semver struct {
	nums [3]uint64
	pre  []string
}

func parseSemver(v string) (semver, bool) {
	var s semver
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	main := v
	if i := strings.IndexByte(v, '-'); i >= 0 {
		main, s.pre = v[:i], strings.Split(v[i+1:], ".")
		for _, p := range s.pre {
			if p == "" {
				return s, false
			}
		}
	}
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return s, false
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return s, false
		}
		s.nums[i] = n
	}
	return s, true
}

func compareSemver(a, b semver) int {
	for i := 0; i < 3; i++ {
		if a.nums[i] != b.nums[i] {
			if a.nums[i] < b.nums[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		if x == y {
			continue
		}
		xn, xerr := strconv.ParseUint(x, 10, 64)
		yn, yerr := strconv.ParseUint(y, 10, 64)
		switch {
		case xerr == nil && yerr == nil:
			if xn < yn {
				return -1
			}
			return 1
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		case x < y:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	}
	return 0
}

func naturalCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si := i
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			sj := j
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case len(a)-i < len(b)-j:
		return -1
	case len(a)-i > len(b)-j:
		return 1
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
