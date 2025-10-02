// main.go
// A tiny Cobra-based CLI that wires into your rdap package.
// Place under ./cmd/rdapctl and run `go run . <subcommand>`.
//
// Subcommands
//   domain, ip, asn, ns, entity, lookup   – fetch a single object
//   tree                                   – recursively flush the entire related graph
//
// Flags
//   --json (default true)     – JSON output for single objects; for tree, outputs a graph {nodes,edges}
//   --walk                    – for single-object commands: print related, one level deep (text mode only)
//   --max-depth               – for `tree` recursion depth (default 5)
//   --follow-links            – for `tree`, chase rdap.Links[] (best-effort)
//   --tld                     – hint for entity/lookup resolution
//
// Env options for client:
//   RDAPCTL_UA, RDAPCTL_TIMEOUT, RDAPCTL_DNS_BOOTSTRAP, RDAPCTL_IP_BOOTSTRAP, RDAPCTL_ASN_BOOTSTRAP
//
// Build
//   go mod init example.com/rdapctl
//   go get github.com/spf13/cobra@latest
//   go get github.com/datum-labs/rdap@latest
//   go build -o rdapctl
//
// Run examples
//   ./rdapctl domain example.com
//   ./rdapctl tree example.com
//   ./rdapctl tree 8.8.8.0/24 --follow-links
//   ./rdapctl lookup ns1.google.com --json=false
//   ./rdapctl entity ORG-GOGL-1 --tld com

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	rc "github.com/datum-labs/rdap"
)

var (
	flagJSON        = true // default to JSON output
	flagWalk        bool
	flagTLD         string
	flagMaxDepth    int
	flagFollowLinks bool
)

func main() {
	root := &cobra.Command{
		Use:   "rdapctl",
		Short: "RDAP CLI",
	}

	// Global flags
	root.PersistentFlags().BoolVar(&flagJSON, "json", true, "emit JSON; set --json=false for text output")
	root.PersistentFlags().BoolVar(&flagWalk, "walk", false, "for single-object commands: resolve immediate related objects (ignored in --json)")
	root.PersistentFlags().StringVar(&flagTLD, "tld", "", "TLD hint for entity lookups (e.g., 'com')")

	// Subcommands
	root.AddCommand(cmdDomain(), cmdIP(), cmdASN(), cmdNS(), cmdEntity(), cmdLookup(), cmdTree())

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

// newClient constructs the rdap.Client with env-configured options.
func newClient() *rc.Client {
	opts := []rc.Option{}
	if ua := os.Getenv("RDAPCTL_UA"); ua != "" {
		opts = append(opts, rc.WithUserAgent(ua))
	}
	if to := os.Getenv("RDAPCTL_TIMEOUT"); to != "" {
		if d, err := time.ParseDuration(to); err == nil {
			opts = append(opts, rc.WithTimeout(d))
		}
	}
	if u := os.Getenv("RDAPCTL_DNS_BOOTSTRAP"); u != "" {
		opts = append(opts, rc.WithBootstrapURL(u))
	}
	if u := os.Getenv("RDAPCTL_IP_BOOTSTRAP"); u != "" {
		opts = append(opts, rc.WithIPBootstrapURL(u))
	}
	if u := os.Getenv("RDAPCTL_ASN_BOOTSTRAP"); u != "" {
		opts = append(opts, rc.WithASNBootstrapURL(u))
	}
	return rc.New(opts...)
}

func cmdDomain() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain <fqdn>",
		Short: "Fetch domain RDAP",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			ctx := context.Background()
			d, err := c.Domain(ctx, args[0])
			if err != nil {
				return err
			}
			return renderObject(c, ctx, d)
		},
	}
	return cmd
}

func cmdIP() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ip <ip|cidr>",
		Short: "Fetch IP network RDAP",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			ctx := context.Background()
			ipn, err := c.IP(ctx, args[0])
			if err != nil {
				return err
			}
			return renderObject(c, ctx, ipn)
		},
	}
	return cmd
}

func cmdASN() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "asn <AS12345|12345>",
		Short: "Fetch autnum RDAP",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			ctx := context.Background()
			a, err := c.Autnum(ctx, args[0])
			if err != nil {
				return err
			}
			return renderObject(c, ctx, a)
		},
	}
	return cmd
}

func cmdNS() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ns <hostname>",
		Short: "Fetch nameserver RDAP",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			ctx := context.Background()
			ns, err := c.Nameserver(ctx, args[0])
			if err != nil {
				return err
			}
			return renderObject(c, ctx, ns)
		},
	}
	return cmd
}

func cmdEntity() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entity <handle>",
		Short: "Fetch entity RDAP (use --tld as a hint)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			ctx := context.Background()
			e, err := c.Entity(ctx, args[0], flagTLD)
			if err != nil {
				return err
			}
			return renderObject(c, ctx, e)
		},
	}
	return cmd
}

func cmdLookup() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lookup <query>",
		Short: "Auto-detect and fetch RDAP (ASN/IP/Domain/NS/Entity)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			ctx := context.Background()
			obj, err := c.Lookup(ctx, args[0], flagTLD)
			if err != nil {
				return err
			}
			return renderObject(c, ctx, obj)
		},
	}
	return cmd
}

// ---- TREE (flush entire graph) ---------------------------------------------

func cmdTree() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tree <seed>",
		Short: "Flush the entire RDAP graph reachable from a seed (domain/ip/asn/ns/entity)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			ctx := context.Background()

			seed := args[0]
			obj, err := c.Lookup(ctx, seed, flagTLD)
			if err != nil {
				return err
			}

			seen := newSeenSet()
			graph := &Graph{Nodes: map[string]GraphNode{}, Edges: []GraphEdge{}}

			if err := walkAny(ctx, c, obj, 0, flagMaxDepth, flagFollowLinks, seen, graph); err != nil {
				return err
			}

			if flagJSON {
				// Emit consolidated graph (nodes keyed by id, edges with from->to)
				return printJSON(graph)
			}

			// Pretty text (depth-first, deterministic-ish using the graph we built)
			printHeader("tree", seed, fmt.Sprintf("(max-depth=%d follow-links=%v) ", flagMaxDepth, flagFollowLinks))
			printGraphText(graph)
			return nil
		},
	}
	cmd.Flags().IntVar(&flagMaxDepth, "max-depth", 5, "maximum recursion depth when walking the graph")
	cmd.Flags().BoolVar(&flagFollowLinks, "follow-links", false, "follow RDAP links[] to fetch additional objects (best-effort)")
	return cmd
}

// Graph types for JSON output
type Graph struct {
	Nodes map[string]GraphNode `json:"nodes"`
	Edges []GraphEdge          `json:"edges"`
}

type GraphNode struct {
	ID   string      `json:"id"`
	Kind string      `json:"kind"` // domain | nameserver | entity | ip-network | autnum | link
	Data interface{} `json:"data"` // the typed RDAP object (Domain, Nameserver, Entity, IPNetwork, Autnum) or link URL
}

type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rel  string `json:"rel"` // e.g., nameserver, entity, parent, link, contact, etc.
}

// ---- Rendering for single objects -----------------------------------------

func renderObject(c *rc.Client, ctx context.Context, obj any) error {
	if flagJSON {
		// In JSON mode, output only the primary typed object.
		// (Note: --walk is ignored in JSON mode to keep output single-object.)
		return printJSON(obj)
	}
	switch v := obj.(type) {
	case *rc.Domain:
		printDomain(v)
		if flagWalk {
			return walkDomainOnce(c, ctx, v)
		}
	case *rc.Nameserver:
		printNameserver(v)
		if flagWalk {
			return walkNameserverOnce(c, ctx, v)
		}
	case *rc.IPNetwork:
		printIPNet(v)
		if flagWalk {
			return walkIPNetOnce(c, ctx, v)
		}
	case *rc.Autnum:
		printAutnum(v)
		if flagWalk {
			return walkAutnumOnce(c, ctx, v)
		}
	case *rc.Entity:
		printEntity(v)
		if flagWalk {
			return walkEntityOnce(c, ctx, v, make(map[string]struct{}))
		}
	default:
		return errors.New("unknown object type")
	}
	return nil
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func printHeader(kind, handle, extra string) {
	fmt.Printf("\n=== %s: %s %s===\n", strings.ToUpper(kind), handle, extra)
}

func printDomain(d *rc.Domain) {
	printHeader("domain", d.LDHName, "")
	fmt.Printf("handle: %s\n", d.Handle)
	if len(d.Status) > 0 {
		fmt.Printf("status: %v\n", d.Status)
	}
	if d.SecureDNS != nil {
		fmt.Printf("dnssec: zoneSigned=%v delegationSigned=%v\n", d.SecureDNS.ZoneSigned, d.SecureDNS.DelegationSigned)
	}
	if len(d.Nameservers) > 0 {
		fmt.Println("nameservers:")
		for _, ns := range d.Nameservers {
			fmt.Printf("  - %s\n", ns.LDHName)
		}
	}
	if len(d.Entities) > 0 {
		fmt.Println("entities:")
		for _, e := range d.Entities {
			fmt.Printf("  - %s (%v)\n", e.Handle, e.Roles)
		}
	}
}

func printNameserver(n *rc.Nameserver) {
	printHeader("nameserver", n.LDHName, "")
	fmt.Printf("handle: %s\n", n.Handle)
	if n.IPAddresses != nil {
		if len(n.IPAddresses.V4) > 0 {
			fmt.Printf("v4: %v\n", n.IPAddresses.V4)
		}
		if len(n.IPAddresses.V6) > 0 {
			fmt.Printf("v6: %v\n", n.IPAddresses.V6)
		}
	}
	if len(n.Entities) > 0 {
		fmt.Println("entities:")
		for _, e := range n.Entities {
			fmt.Printf("  - %s (%v)\n", e.Handle, e.Roles)
		}
	}
}

func printIPNet(n *rc.IPNetwork) {
	printHeader("ip network", n.Handle, fmt.Sprintf("(%s %s-%s) ", n.IPVersion, n.StartAddress, n.EndAddress))
	fmt.Printf("name: %s country: %s parent: %s\n", n.Name, n.Country, n.ParentHandle)
}

func printAutnum(a *rc.Autnum) {
	printHeader("autnum", a.Handle, fmt.Sprintf("(%d-%d) ", a.StartAutnum, a.EndAutnum))
	fmt.Printf("name: %s country: %s type: %s\n", a.Name, a.Country, a.Type)
}

func printEntity(e *rc.Entity) {
	printHeader("entity", e.Handle, "")
	if len(e.Roles) > 0 {
		fmt.Printf("roles: %v\n", e.Roles)
	}
}

// ---- One-level walks for single-object commands ---------------------------

func walkDomainOnce(c *rc.Client, ctx context.Context, d *rc.Domain) error {
	for _, ns := range d.Nameservers {
		fmt.Printf("\n> resolving nameserver %s...\n", ns.LDHName)
		full, err := c.Nameserver(ctx, ns.LDHName)
		if err != nil {
			fmt.Printf("  (error: %v)\n", err)
			continue
		}
		printNameserver(full)
	}
	for _, e := range d.Entities {
		fmt.Printf("\n> resolving entity %s...\n", e.Handle)
		if err := walkEntityOnce(c, ctx, &e, make(map[string]struct{})); err != nil {
			fmt.Printf("  (error: %v)\n", err)
		}
	}
	return nil
}

func walkNameserverOnce(c *rc.Client, ctx context.Context, n *rc.Nameserver) error {
	for _, e := range n.Entities {
		fmt.Printf("\n> resolving entity %s...\n", e.Handle)
		if err := walkEntityOnce(c, ctx, &e, make(map[string]struct{})); err != nil {
			fmt.Printf("  (error: %v)\n", err)
		}
	}
	return nil
}

func walkIPNetOnce(c *rc.Client, ctx context.Context, n *rc.IPNetwork) error {
	if n.ParentHandle != "" {
		fmt.Printf("\n> parent handle %s present (fetch via registry-specific link if provided)\n", n.ParentHandle)
	}
	for _, e := range n.Entities {
		fmt.Printf("\n> resolving entity %s...\n", e.Handle)
		if err := walkEntityOnce(c, ctx, &e, make(map[string]struct{})); err != nil {
			fmt.Printf("  (error: %v)\n", err)
		}
	}
	return nil
}

func walkAutnumOnce(c *rc.Client, ctx context.Context, a *rc.Autnum) error {
	for _, e := range a.Entities {
		fmt.Printf("\n> resolving entity %s...\n", e.Handle)
		if err := walkEntityOnce(c, ctx, &e, make(map[string]struct{})); err != nil {
			fmt.Printf("  (error: %v)\n", err)
		}
	}
	return nil
}

func walkEntityOnce(c *rc.Client, ctx context.Context, e *rc.Entity, seen map[string]struct{}) error {
	if e == nil {
		return nil
	}
	if e.Handle != "" {
		if _, dup := seen[e.Handle]; dup {
			return nil
		}
		seen[e.Handle] = struct{}{}
	}
	printEntity(e)
	for _, a := range e.Autnums {
		fmt.Printf("\n> nested autnum %s...\n", a.Handle)
		printAutnum(&a)
	}
	for _, n := range e.Networks {
		fmt.Printf("\n> nested network %s...\n", n.Handle)
		printIPNet(&n)
	}
	return nil
}

// ---- Full graph walk (tree) -----------------------------------------------

type seenSet struct {
	ids map[string]struct{}
}

func newSeenSet() *seenSet { return &seenSet{ids: map[string]struct{}{}} }

func (s *seenSet) add(id string) bool {
	if _, ok := s.ids[id]; ok {
		return false
	}
	s.ids[id] = struct{}{}
	return true
}

func makeNodeID(kind, key string) string { return kind + ":" + strings.ToLower(key) }

func walkAny(ctx context.Context, c *rc.Client, obj any, depth, maxDepth int, followLinks bool, seen *seenSet, g *Graph) error {
	if obj == nil || depth > maxDepth {
		return nil
	}
	switch v := obj.(type) {
	case *rc.Domain:
		id := makeNodeID("domain", v.LDHName)
		if seen.add(id) {
			addNode(g, id, "domain", v)
			// Nameservers
			for _, ns := range v.Nameservers {
				nsObj, err := c.Nameserver(ctx, ns.LDHName)
				if err == nil && nsObj != nil {
					nsID := makeNodeID("nameserver", nsObj.LDHName)
					addEdge(g, id, nsID, "nameserver")
					_ = walkAny(ctx, c, nsObj, depth+1, maxDepth, followLinks, seen, g)
				}
			}
			// Entities
			for _, e := range v.Entities {
				ent, err := c.Entity(ctx, e.Handle, "")
				if err == nil && ent != nil {
					entID := makeNodeID("entity", ent.Handle)
					addEdge(g, id, entID, "entity")
					_ = walkAny(ctx, c, ent, depth+1, maxDepth, followLinks, seen, g)
				}
			}
			// Links (optional)
			if followLinks {
				walkLinks(ctx, c, id, v.Links, depth, maxDepth, seen, g)
			}
		}
	case *rc.Nameserver:
		id := makeNodeID("nameserver", v.LDHName)
		if seen.add(id) {
			addNode(g, id, "nameserver", v)
			for _, e := range v.Entities {
				ent, err := c.Entity(ctx, e.Handle, "")
				if err == nil && ent != nil {
					entID := makeNodeID("entity", ent.Handle)
					addEdge(g, id, entID, "entity")
					_ = walkAny(ctx, c, ent, depth+1, maxDepth, followLinks, seen, g)
				}
			}
			if followLinks {
				walkLinks(ctx, c, id, v.Links, depth, maxDepth, seen, g)
			}
		}
	case *rc.IPNetwork:
		id := makeNodeID("ip-network", v.Handle)
		if seen.add(id) {
			addNode(g, id, "ip-network", v)
			for _, e := range v.Entities {
				ent, err := c.Entity(ctx, e.Handle, "")
				if err == nil && ent != nil {
					entID := makeNodeID("entity", ent.Handle)
					addEdge(g, id, entID, "entity")
					_ = walkAny(ctx, c, ent, depth+1, maxDepth, followLinks, seen, g)
				}
			}
			if followLinks {
				walkLinks(ctx, c, id, v.Links, depth, maxDepth, seen, g)
			}
		}
	case *rc.Autnum:
		id := makeNodeID("autnum", v.Handle)
		if seen.add(id) {
			addNode(g, id, "autnum", v)
			for _, e := range v.Entities {
				ent, err := c.Entity(ctx, e.Handle, "")
				if err == nil && ent != nil {
					entID := makeNodeID("entity", ent.Handle)
					addEdge(g, id, entID, "entity")
					_ = walkAny(ctx, c, ent, depth+1, maxDepth, followLinks, seen, g)
				}
			}
			if followLinks {
				walkLinks(ctx, c, id, v.Links, depth, maxDepth, seen, g)
			}
		}
	case *rc.Entity:
		id := makeNodeID("entity", v.Handle)
		if seen.add(id) {
			addNode(g, id, "entity", v)
			for _, a := range v.Autnums {
				full, err := c.Autnum(ctx, a.Handle)
				if err == nil && full != nil {
					to := makeNodeID("autnum", full.Handle)
					addEdge(g, id, to, "autnum")
					_ = walkAny(ctx, c, full, depth+1, maxDepth, followLinks, seen, g)
				}
			}
			for _, n := range v.Networks {
				full, err := c.IP(ctx, n.Handle)
				if err == nil && full != nil {
					to := makeNodeID("ip-network", full.Handle)
					addEdge(g, id, to, "network")
					_ = walkAny(ctx, c, full, depth+1, maxDepth, followLinks, seen, g)
				}
			}
			if followLinks {
				walkLinks(ctx, c, id, v.Links, depth, maxDepth, seen, g)
			}
		}
	default:
		return errors.New("unknown seed type")
	}
	return nil
}

// walkLinks tries to follow RDAP link relations that look like domain/entity/ns/autnum/ip.
// This is best-effort and safe-guards with parsing & small pattern matches.
func walkLinks(ctx context.Context, c *rc.Client, fromID string, links []rc.Link, depth, maxDepth int, seen *seenSet, g *Graph) {
	for _, l := range links {
		if l.Href == "" {
			continue
		}
		u, err := url.Parse(l.Href)
		if err != nil || u.Path == "" {
			continue
		}
		// Common RDAP paths: /domain/<name> /entity/<handle> /nameserver/<name> /autnum/<n> /ip/<cidr>
		path := strings.ToLower(u.Path)
		switch {
		case strings.Contains(path, "/domain/"):
			name := tail(path)
			if name == "" {
				break
			}
			if dom, err := c.Domain(ctx, name); err == nil && dom != nil {
				to := makeNodeID("domain", dom.LDHName)
				addEdge(g, fromID, to, "link:"+relOr("domain", l.Rel))
				_ = walkAny(ctx, c, dom, depth+1, maxDepth, true, seen, g)
			}
		case strings.Contains(path, "/nameserver/"):
			name := tail(path)
			if name == "" {
				break
			}
			if ns, err := c.Nameserver(ctx, name); err == nil && ns != nil {
				to := makeNodeID("nameserver", ns.LDHName)
				addEdge(g, fromID, to, "link:"+relOr("nameserver", l.Rel))
				_ = walkAny(ctx, c, ns, depth+1, maxDepth, true, seen, g)
			}
		case strings.Contains(path, "/entity/"):
			h := tail(path)
			if h == "" {
				break
			}
			if ent, err := c.Entity(ctx, h, ""); err == nil && ent != nil {
				to := makeNodeID("entity", ent.Handle)
				addEdge(g, fromID, to, "link:"+relOr("entity", l.Rel))
				_ = walkAny(ctx, c, ent, depth+1, maxDepth, true, seen, g)
			}
		case strings.Contains(path, "/autnum/"):
			h := tail(path)
			if h == "" {
				break
			}
			if a, err := c.Autnum(ctx, h); err == nil && a != nil {
				to := makeNodeID("autnum", a.Handle)
				addEdge(g, fromID, to, "link:"+relOr("autnum", l.Rel))
				_ = walkAny(ctx, c, a, depth+1, maxDepth, true, seen, g)
			}
		case strings.Contains(path, "/ip/"):
			h := tail(path)
			if h == "" {
				break
			}
			if n, err := c.IP(ctx, h); err == nil && n != nil {
				to := makeNodeID("ip-network", n.Handle)
				addEdge(g, fromID, to, "link:"+relOr("ip", l.Rel))
				_ = walkAny(ctx, c, n, depth+1, maxDepth, true, seen, g)
			}
		default:
			// Ignore other link types quietly
		}
	}
}

var slashTail = regexp.MustCompile(`/([^/]+)$`)

func tail(p string) string {
	m := slashTail.FindStringSubmatch(p)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func relOr(def, rel string) string {
	if rel == "" {
		return def
	}
	return rel
}

func addNode(g *Graph, id, kind string, data interface{}) {
	if _, ok := g.Nodes[id]; ok {
		return
	}
	g.Nodes[id] = GraphNode{ID: id, Kind: kind, Data: data}
}

func addEdge(g *Graph, from, to, rel string) {
	g.Edges = append(g.Edges, GraphEdge{From: from, To: to, Rel: rel})
}

// Text presentation of the graph (simple fan-out by kind, then ID)
func printGraphText(g *Graph) {
	// Group by kind
	kinds := map[string][]GraphNode{}
	for _, n := range g.Nodes {
		kinds[n.Kind] = append(kinds[n.Kind], n)
	}

	order := []string{"domain", "nameserver", "entity", "ip-network", "autnum", "link"}
	for _, k := range order {
		nodes := kinds[k]
		if len(nodes) == 0 {
			continue
		}
		fmt.Printf("\n[%s]\n", strings.ToUpper(k))
		for _, n := range nodes {
			fmt.Printf("- %s\n", n.ID)
			// show outward edges
			for _, e := range g.Edges {
				if e.From == n.ID {
					fmt.Printf("    -> %s (%s)\n", e.To, e.Rel)
				}
			}
		}
	}
}
