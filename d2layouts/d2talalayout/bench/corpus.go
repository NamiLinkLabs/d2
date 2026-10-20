package bench

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
)

// CorpusSpec describes one generated benchmark diagram.
type CorpusSpec struct {
	// Name is the corpus file stem, for example "arch_060".
	Name string
	// Objects is the exact number of D2 objects (containers and leaves).
	Objects int
	// Seed drives the deterministic generator.
	Seed int64
}

// StandardCorpus lists the committed benchmark diagrams. Object counts include
// containers.
var StandardCorpus = []CorpusSpec{
	{Name: "arch_010", Objects: 10, Seed: 10},
	{Name: "arch_060", Objects: 60, Seed: 60},
	// The largest size the unmodified engine completes on this family.
	{Name: "arch_100", Objects: 100, Seed: 100},
	{Name: "arch_200", Objects: 200, Seed: 200},
	{Name: "arch_500", Objects: 500, Seed: 500},
}

// EditDirective prefixes the one-line edit recorded in every corpus file. The
// harness appends the rest of that comment line to the source to measure
// layout stability.
const EditDirective = "# bench-edit: "

var domainNames = []string{
	"orders", "payments", "users", "catalog", "search", "inventory", "shipping",
	"billing", "notifications", "analytics", "reviews", "recommendations",
	"profile", "cart", "checkout", "pricing", "fraud", "ledger", "reporting",
	"accounts", "sessions", "media", "messaging", "returns", "warehouse",
	"loyalty", "promotions", "tax", "invoicing", "subscriptions", "audit",
	"compliance", "support", "tickets", "chat", "feeds", "geo", "routing",
	"dispatch", "fleet", "drivers", "partners", "vendors", "contracts",
	"documents", "exports", "imports", "scheduling", "calendar", "metrics",
}

var areaNames = []string{
	"commerce", "fulfillment", "identity", "insights", "engagement", "finance",
	"operations", "partner", "content", "logistics", "trust", "growth",
}

// component kinds in the order a domain acquires them.
type componentKind struct {
	id    string
	shape string
}

var componentKinds = []componentKind{
	{"api", ""},
	{"db", "cylinder"},
	{"worker", ""},
	{"cache", "stored_data"},
	{"queue", "queue"},
	{"ui", ""},
	{"scheduler", ""},
	{"indexer", ""},
	{"replica", "cylinder"},
	{"admin", ""},
}

type leaf struct {
	path  string
	shape string
	label string
}

type domain struct {
	path   string
	label  string
	leaves []leaf
	has    map[string]string // component kind -> path
}

type genEdge struct {
	src, dst, label string
}

type generator struct {
	rng     *rand.Rand
	lines   []string
	edges   []genEdge
	edgeSet map[[2]string]bool
	count   int
}

func (g *generator) emit(format string, args ...any) {
	g.lines = append(g.lines, fmt.Sprintf(format, args...))
}

func (g *generator) connect(src, dst, label string) {
	if src == "" || dst == "" || src == dst {
		return
	}
	key := [2]string{src, dst}
	if g.edgeSet[key] || g.edgeSet[[2]string{dst, src}] {
		return
	}
	g.edgeSet[key] = true
	g.edges = append(g.edges, genEdge{src: src, dst: dst, label: label})
}

func (g *generator) maybeLabel(p float64, labels ...string) string {
	if g.rng.Float64() >= p {
		return ""
	}
	return labels[g.rng.Intn(len(labels))]
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func join(prefix, id string) string {
	if prefix == "" {
		return id
	}
	return prefix + "." + id
}

// Generate deterministically produces a software-architecture D2 diagram
// with exactly spec.Objects objects, plus the one-line edit that adds an edge
// between two existing, previously unconnected leaves.
func Generate(spec CorpusSpec) (source string, edit string, err error) {
	if spec.Objects < 6 {
		return "", "", fmt.Errorf("corpus %s: need at least 6 objects", spec.Name)
	}
	g := &generator{
		rng:     rand.New(rand.NewSource(spec.Seed)),
		edgeSet: map[[2]string]bool{},
	}
	n := spec.Objects

	// Tier sizes scale with the diagram. Each costs objects from the budget.
	numClients := min(max(1, n/40), 4)
	hasEdge := n >= 20
	hasData := n >= 30
	hasExternal := n >= 40
	useAreas := n >= 150
	wrapServices := !useAreas && n >= 20

	budget := n - numClients
	edgeLeaves := []string{}
	if hasEdge {
		edgeLeaves = []string{"cdn", "lb", "gateway"}
		if n >= 200 {
			edgeLeaves = append(edgeLeaves, "waf")
		}
		budget -= 1 + len(edgeLeaves)
	}
	dataLeaves := []leaf{}
	if hasData {
		dataLeaves = []leaf{{path: "kafka", shape: "queue"}, {path: "redis", shape: "stored_data"}, {path: "s3", shape: "cylinder"}}
		if n >= 200 {
			dataLeaves = append(dataLeaves, leaf{path: "warehouse", shape: "cylinder"}, leaf{path: "search_cluster", shape: "cylinder"})
		}
		budget -= 1 + len(dataLeaves)
	}
	externalLeaves := []string{}
	if hasExternal {
		externalLeaves = []string{"stripe", "email", "idp"}
		if n >= 200 {
			externalLeaves = append(externalLeaves, "maps", "sms")
		}
		budget -= 1 + len(externalLeaves)
	}
	if wrapServices {
		budget--
	}

	// Plan domains (and areas) to consume the remaining budget exactly.
	type plannedDomain struct {
		area   int
		leaves int
	}
	var planned []plannedDomain
	areaCount := 0
	areaDomains := 0
	areaTarget := 0
	for budget > 0 {
		if useAreas && (areaCount == 0 || areaDomains >= areaTarget) {
			if budget < 4 {
				break
			}
			areaCount++
			areaDomains = 0
			areaTarget = 4 + g.rng.Intn(5)
			budget--
		}
		leaves := 3 + g.rng.Intn(5)
		if n < 20 {
			leaves = 3 + g.rng.Intn(2)
		}
		if budget-1-leaves < 3 {
			leaves = budget - 1
		}
		if leaves < 2 {
			break
		}
		planned = append(planned, plannedDomain{area: areaCount - 1, leaves: leaves})
		areaDomains++
		budget -= 1 + leaves
	}
	// Any leftover objects become extra leaves on the last domain.
	if budget > 0 {
		if len(planned) == 0 {
			return "", "", fmt.Errorf("corpus %s: budget too small", spec.Name)
		}
		planned[len(planned)-1].leaves += budget
		budget = 0
	}

	g.emit("# Generated by d2layouts/d2talalayout/bench (go test ./d2layouts/d2talalayout/bench -run TestCorpusFiles -update).")
	g.emit("# %s: %d objects, seed %d. Do not edit by hand.", spec.Name, n, spec.Seed)
	g.emit("")

	clients := []leaf{}
	clientKinds := []leaf{{path: "customer", shape: "person"}, {path: "mobile", shape: ""}, {path: "admin", shape: "person"}, {path: "partner_app", shape: ""}}
	for i := 0; i < numClients; i++ {
		clients = append(clients, clientKinds[i])
	}
	for _, c := range clients {
		if c.shape != "" {
			g.emit("%s: {shape: %s}", c.path, c.shape)
		} else {
			g.emit("%s", c.path)
		}
		g.count++
	}
	g.emit("")

	if hasEdge {
		g.emit("edge: Edge {")
		g.count++
		for _, l := range edgeLeaves {
			g.emit("  %s", l)
			g.count++
		}
		g.emit("}")
		g.emit("")
	}

	// Build domains.
	domainPrefix := ""
	if wrapServices {
		domainPrefix = "services"
	}
	areaPerm := g.rng.Perm(len(areaNames))
	domainPerm := g.rng.Perm(len(domainNames))
	domains := make([]*domain, 0, len(planned))
	areaPaths := []string{}
	for i, pd := range planned {
		prefix := domainPrefix
		if useAreas {
			for len(areaPaths) <= pd.area {
				areaPaths = append(areaPaths, areaNames[areaPerm[len(areaPaths)%len(areaNames)]])
				if len(areaPaths) > len(areaNames) {
					areaPaths[len(areaPaths)-1] += fmt.Sprintf("%d", len(areaPaths))
				}
			}
			prefix = areaPaths[pd.area]
		}
		name := domainNames[domainPerm[i%len(domainNames)]]
		if i >= len(domainNames) {
			name += fmt.Sprintf("%d", i/len(domainNames)+1)
		}
		d := &domain{path: join(prefix, name), label: title(name), has: map[string]string{}}
		for k := 0; k < pd.leaves; k++ {
			kind := componentKinds[k%len(componentKinds)]
			id := kind.id
			if k >= len(componentKinds) {
				id += fmt.Sprintf("%d", k/len(componentKinds)+1)
			}
			l := leaf{path: join(d.path, id), shape: kind.shape}
			d.leaves = append(d.leaves, l)
			if _, ok := d.has[kind.id]; !ok {
				d.has[kind.id] = l.path
			}
		}
		domains = append(domains, d)
	}

	emitDomain := func(d *domain, indent string) {
		local := d.path[strings.LastIndex(d.path, ".")+1:]
		g.emit("%s%s: %s {", indent, local, d.label)
		g.count++
		for _, l := range d.leaves {
			id := l.path[strings.LastIndex(l.path, ".")+1:]
			if l.shape != "" {
				g.emit("%s  %s: {shape: %s}", indent, id, l.shape)
			} else {
				g.emit("%s  %s", indent, id)
			}
			g.count++
		}
		g.emit("%s}", indent)
	}

	if wrapServices {
		g.emit("services: Services {")
		g.count++
		for _, d := range domains {
			emitDomain(d, "  ")
		}
		g.emit("}")
		g.emit("")
	} else if useAreas {
		for ai, area := range areaPaths {
			g.emit("%s: %s {", area, title(area))
			g.count++
			for di, d := range domains {
				if planned[di].area == ai {
					emitDomain(d, "  ")
				}
			}
			g.emit("}")
			g.emit("")
		}
	} else {
		for _, d := range domains {
			emitDomain(d, "")
		}
		g.emit("")
	}

	if hasData {
		g.emit("data: Shared data {")
		g.count++
		for _, l := range dataLeaves {
			g.emit("  %s: {shape: %s}", l.path, l.shape)
			g.count++
		}
		g.emit("}")
		g.emit("")
	}
	if hasExternal {
		g.emit("external: External {")
		g.count++
		for _, l := range externalLeaves {
			g.emit("  %s: {shape: cloud}", l)
			g.count++
		}
		g.emit("}")
		g.emit("")
	}
	if g.count != n {
		return "", "", fmt.Errorf("corpus %s: generated %d objects, want %d", spec.Name, g.count, n)
	}

	// Intra-domain edges follow a typical service shape.
	for _, d := range domains {
		h := d.has
		g.connect(h["ui"], h["api"], g.maybeLabel(0.3, "HTTPS", "REST"))
		g.connect(h["admin"], h["api"], "")
		g.connect(h["api"], h["db"], g.maybeLabel(0.3, "SQL", "reads/writes"))
		g.connect(h["api"], h["cache"], g.maybeLabel(0.2, "get/set"))
		g.connect(h["api"], h["queue"], g.maybeLabel(0.3, "enqueue"))
		g.connect(h["queue"], h["worker"], g.maybeLabel(0.3, "consume"))
		if h["queue"] == "" {
			g.connect(h["api"], h["worker"], g.maybeLabel(0.3, "jobs"))
		}
		g.connect(h["worker"], h["db"], "")
		g.connect(h["scheduler"], h["worker"], g.maybeLabel(0.3, "cron"))
		g.connect(h["indexer"], h["db"], g.maybeLabel(0.3, "CDC"))
		g.connect(h["db"], h["replica"], g.maybeLabel(0.5, "replication"))
		// Extra numbered components hang off the api.
		for _, l := range d.leaves {
			last := l.path[strings.LastIndex(l.path, ".")+1:]
			if strings.IndexFunc(last, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0 {
				g.connect(h["api"], l.path, "")
			}
		}
		// Domains without an api still need some internal structure.
		if h["api"] != "" && h["worker"] == "" && len(d.leaves) == 2 {
			g.connect(d.leaves[0].path, d.leaves[1].path, "")
		}
	}

	// Cross-domain calls, mostly downstream, sometimes upstream.
	for i, d := range domains {
		calls := 1 + g.rng.Intn(2)
		for c := 0; c < calls && len(domains) > 1; c++ {
			j := i
			for j == i {
				if g.rng.Float64() < 0.15 || i == len(domains)-1 {
					j = g.rng.Intn(len(domains))
				} else {
					span := min(len(domains)-i-1, 6)
					j = i + 1 + g.rng.Intn(span)
				}
			}
			g.connect(d.has["api"], domains[j].has["api"], g.maybeLabel(0.35, "gRPC", "HTTP", "calls"))
		}
	}

	// Entry points.
	if hasEdge {
		for _, c := range clients {
			g.connect(c.path, "edge.cdn", g.maybeLabel(0.3, "HTTPS"))
		}
		if slices.Contains(edgeLeaves, "waf") {
			g.connect("edge.cdn", "edge.waf", "")
			g.connect("edge.waf", "edge.lb", "")
		} else {
			g.connect("edge.cdn", "edge.lb", "")
		}
		g.connect("edge.lb", "edge.gateway", "")
		public := max(1, len(domains)*3/10)
		step := max(1, len(domains)/public)
		for i := 0; i < len(domains); i += step {
			target := domains[i].has["ui"]
			if target == "" {
				target = domains[i].has["api"]
			}
			g.connect("edge.gateway", target, g.maybeLabel(0.25, "routes"))
		}
	} else {
		for ci, c := range clients {
			target := domains[ci%len(domains)]
			dst := target.has["ui"]
			if dst == "" {
				dst = target.has["api"]
			}
			g.connect(c.path, dst, g.maybeLabel(0.5, "HTTPS"))
		}
	}

	if hasData {
		for i, d := range domains {
			switch {
			case d.has["worker"] != "" && i%2 == 0:
				g.connect(d.has["api"], "data.kafka", g.maybeLabel(0.4, "publishes"))
			case d.has["worker"] != "":
				g.connect("data.kafka", d.has["worker"], g.maybeLabel(0.4, "subscribes"))
			case i%3 == 0:
				g.connect(d.has["api"], "data.redis", "")
			}
			if i%5 == 4 {
				g.connect(d.has["api"], "data.s3", g.maybeLabel(0.3, "objects"))
			}
			if i%7 == 3 && slices.ContainsFunc(dataLeaves, func(l leaf) bool { return l.path == "warehouse" }) {
				g.connect(d.has["db"], "data.warehouse", g.maybeLabel(0.5, "ETL"))
			}
			if i%9 == 5 && slices.ContainsFunc(dataLeaves, func(l leaf) bool { return l.path == "search_cluster" }) && d.has["indexer"] != "" {
				g.connect(d.has["indexer"], "data.search_cluster", "")
			}
		}
	}

	if hasExternal {
		for i, ext := range externalLeaves {
			// Spread third-party dependencies over the domain list.
			d := domains[(i*7+3)%len(domains)]
			g.connect(d.has["api"], "external."+ext, g.maybeLabel(0.4, "API"))
			if ext == "stripe" {
				g.connect("external.stripe", d.has["api"], "webhook")
			}
			if ext == "idp" && hasEdge {
				g.connect("edge.gateway", "external.idp", "OIDC")
			}
		}
	}

	for _, e := range g.edges {
		if e.label != "" {
			g.emit("%s -> %s: %s", e.src, e.dst, e.label)
		} else {
			g.emit("%s -> %s", e.src, e.dst)
		}
	}

	// The edit connects two existing leaves that are far apart in the
	// declaration order and not yet connected.
	src := domains[len(domains)/3].has["api"]
	var dst string
	for k := 0; k < len(domains); k++ {
		cand := domains[(2*len(domains)/3+k)%len(domains)]
		for _, l := range cand.leaves {
			if l.path != src && !g.edgeSet[[2]string{src, l.path}] && !g.edgeSet[[2]string{l.path, src}] {
				dst = l.path
				break
			}
		}
		if dst != "" {
			break
		}
	}
	if src == "" || dst == "" {
		return "", "", fmt.Errorf("corpus %s: no edit candidate", spec.Name)
	}
	edit = fmt.Sprintf("%s -> %s", src, dst)
	g.emit("")
	g.emit("%s%s", EditDirective, edit)
	return strings.Join(g.lines, "\n") + "\n", edit, nil
}

// EditFromSource extracts the one-line edit recorded in a corpus file.
func EditFromSource(source string) (string, bool) {
	for _, line := range strings.Split(source, "\n") {
		if rest, ok := strings.CutPrefix(line, EditDirective); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// ApplyEdit returns source with the recorded edit appended as one new line.
func ApplyEdit(source, edit string) string {
	if !strings.HasSuffix(source, "\n") {
		source += "\n"
	}
	return source + edit + "\n"
}
