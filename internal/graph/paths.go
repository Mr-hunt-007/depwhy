package graph

import (
	"container/heap"
	"sort"
)

// Step is one node on a path together with the kind of the edge that led to
// it (empty for the first step, which is always a root).
type Step struct {
	Node *Node
	Kind string
}

// PathResult is the outcome of a path search.
type PathResult struct {
	Paths    [][]Step // shortest first, at most the requested maximum
	Total    int      // number of paths found
	Complete bool     // false when the search budget ran out; Total is then a lower bound
}

// DefaultBudget bounds the number of partial paths the search creates. Real
// lockfiles stay far below it; pathological graphs report a lower bound.
const DefaultBudget = 1_000_000

type suffix struct {
	node  string
	kind  string // kind of the edge from node to next.node
	next  *suffix
	depth int // nodes in this suffix
	prio  int // depth - 1 + distance from a root to node: the shortest possible full path
	seq   int
}

func (s *suffix) contains(id string) bool {
	for c := s; c != nil; c = c.next {
		if c.node == id {
			return true
		}
	}
	return false
}

// suffixHeap orders partial paths by the shortest full path they can still
// become, preferring longer suffixes on ties so complete paths surface early,
// then insertion order for stable output.
type suffixHeap []*suffix

func (h suffixHeap) Len() int { return len(h) }
func (h suffixHeap) Less(i, j int) bool {
	if h[i].prio != h[j].prio {
		return h[i].prio < h[j].prio
	}
	if h[i].depth != h[j].depth {
		return h[i].depth > h[j].depth
	}
	return h[i].seq < h[j].seq
}
func (h suffixHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *suffixHeap) Push(x any)   { *h = append(*h, x.(*suffix)) }
func (h *suffixHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return x
}

type reverseEdge struct {
	from string
	kind string
}

func (g *Graph) reverse(skipKind string) map[string][]reverseEdge {
	rev := map[string][]reverseEdge{}
	for from, edges := range g.Edges {
		if _, ok := g.Nodes[from]; !ok {
			continue
		}
		for _, e := range edges {
			if skipKind != "" && e.Kind == skipKind {
				continue
			}
			if _, ok := g.Nodes[e.To]; !ok {
				continue
			}
			rev[e.To] = append(rev[e.To], reverseEdge{from: from, kind: e.Kind})
		}
	}
	for to := range rev {
		list := rev[to]
		sort.Slice(list, func(i, j int) bool {
			a, b := g.Nodes[list[i].from], g.Nodes[list[j].from]
			if a.Root != b.Root {
				return a.Root
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			if a.Version != b.Version {
				return CompareVersions(a.Version, b.Version) < 0
			}
			return a.ID < b.ID
		})
	}
	return rev
}

// rootDistance returns the length of the shortest edge path from any root to
// each node reachable from one, following edges that are not skipped.
func (g *Graph) rootDistance(skipKind string) map[string]int {
	dist := map[string]int{}
	var queue []string
	for _, id := range g.Roots() {
		dist[id] = 0
		queue = append(queue, id)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.Edges[cur] {
			if skipKind != "" && e.Kind == skipKind {
				continue
			}
			if _, ok := g.Nodes[e.To]; !ok {
				continue
			}
			if _, seen := dist[e.To]; !seen {
				dist[e.To] = dist[cur] + 1
				queue = append(queue, e.To)
			}
		}
	}
	return dist
}

// Paths finds every simple path from a root to any of the target nodes,
// shortest first. A path ends at the first root it reaches: roots are reasons
// in themselves. max <= 0 keeps every path. budget <= 0 uses DefaultBudget.
// Edges of g.DeferredKind are only used when no path exists without them.
func (g *Graph) Paths(targets []string, max, budget int) PathResult {
	if budget <= 0 {
		budget = DefaultBudget
	}
	if g.DeferredKind != "" {
		res := g.paths(targets, max, budget, g.DeferredKind)
		if res.Total > 0 || !res.Complete {
			return res
		}
	}
	return g.paths(targets, max, budget, "")
}

// paths runs a best-first search backwards from the targets. The priority of
// a partial path is its length plus the exact distance from a root to its
// head, a lower bound on any full path it can become, so complete paths are
// found in order of length and partial paths that cannot reach a root are
// never expanded.
func (g *Graph) paths(targets []string, max, budget int, skipKind string) PathResult {
	rev := g.reverse(skipKind)
	dist := g.rootDistance(skipKind)
	res := PathResult{Complete: true}
	h := &suffixHeap{}
	seq := 0
	for _, t := range targets {
		d, ok := dist[t]
		if !ok {
			continue // not reachable from any root
		}
		heap.Push(h, &suffix{node: t, depth: 1, prio: d, seq: seq})
		seq++
	}
	for h.Len() > 0 {
		cur := heap.Pop(h).(*suffix)
		if g.Nodes[cur.node].Root {
			res.Total++
			if max <= 0 || len(res.Paths) < max {
				res.Paths = append(res.Paths, g.materialize(cur))
			}
			continue
		}
		for _, r := range rev[cur.node] {
			d, ok := dist[r.from]
			if !ok || cur.contains(r.from) {
				continue
			}
			if seq >= budget {
				res.Complete = false
				return res
			}
			heap.Push(h, &suffix{node: r.from, kind: r.kind, next: cur, depth: cur.depth + 1, prio: cur.depth + d, seq: seq})
			seq++
		}
	}
	return res
}

func (g *Graph) materialize(s *suffix) []Step {
	var steps []Step
	kind := ""
	for c := s; c != nil; c = c.next {
		steps = append(steps, Step{Node: g.Nodes[c.node], Kind: kind})
		kind = c.kind
	}
	return steps
}
