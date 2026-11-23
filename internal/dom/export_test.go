package dom

import "golang.org/x/net/html"

// countCost returns the node cost of a parsed tree as Convert charges it.
func countCost(root *html.Node) int64 {
	nodes, _, _ := (&converter{}).count(root)
	return nodes
}
