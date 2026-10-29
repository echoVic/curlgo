package core

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const MaxGroups = 5000
const MaxGroupDepth = 32

type RequestGroup struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
}

// ValidateRequestGroups checks IDs and the parent graph without resolving requests.
// Duplicate names are allowed because imported clients can contain them.
func ValidateRequestGroups(groups []RequestGroup) error {
	if len(groups) > MaxGroups {
		return fmt.Errorf("分组最多 %d 个", MaxGroups)
	}
	index := make(map[string]RequestGroup, len(groups))
	for _, g := range groups {
		invalid := func(s string) bool {
			return !utf8.ValidString(s) || strings.IndexFunc(s, func(r rune) bool { return r < 32 || r == 127 }) >= 0
		}
		if g.ID == "" || len(g.ID) > 256 || invalid(g.ID) || strings.TrimSpace(g.Name) == "" || len(g.Name) > 1024 || invalid(g.Name) {
			return fmt.Errorf("分组 ID 或名称无效")
		}
		if _, found := index[g.ID]; found {
			return fmt.Errorf("分组 ID 重复")
		}
		index[g.ID] = g
	}
	for _, g := range groups {
		id := g.ID
		seen := map[string]bool{}
		for depth := 0; id != ""; depth++ {
			if seen[id] {
				return fmt.Errorf("分组不能循环嵌套")
			}
			if depth >= MaxGroupDepth {
				return fmt.Errorf("分组层级最多 %d 层", MaxGroupDepth)
			}
			parent, found := index[id]
			if !found {
				return fmt.Errorf("分组的父节点不存在")
			}
			seen[id] = true
			id = parent.ParentID
		}
	}
	return nil
}

func GroupSubtree(groups []RequestGroup, id string) map[string]bool {
	children := make(map[string][]string, len(groups))
	found := false
	for _, g := range groups {
		children[g.ParentID] = append(children[g.ParentID], g.ID)
		found = found || g.ID == id
	}
	out := map[string]bool{}
	if !found {
		return out
	}
	queue := []string{id}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if out[next] {
			continue
		}
		out[next] = true
		queue = append(queue, children[next]...)
	}
	return out
}

// GroupPaths builds the display index once for large lists and search filters.
func GroupPaths(groups []RequestGroup) map[string]string {
	children := make(map[string][]RequestGroup, len(groups))
	for _, g := range groups {
		children[g.ParentID] = append(children[g.ParentID], g)
	}
	paths := make(map[string]string, len(groups))
	var walk func(string, string, int)
	walk = func(parent, prefix string, depth int) {
		if depth >= MaxGroupDepth {
			return
		}
		for _, g := range children[parent] {
			path := g.Name
			if prefix != "" {
				path = prefix + " / " + path
			}
			paths[g.ID] = path
			walk(g.ID, path, depth+1)
		}
	}
	walk("", "", 0)
	return paths
}

func GroupPath(groups []RequestGroup, id string) string {
	index := make(map[string]RequestGroup, len(groups))
	for _, g := range groups {
		index[g.ID] = g
	}
	if _, found := index[id]; !found {
		return "未分组"
	}
	var names []string
	for depth := 0; id != "" && depth < MaxGroupDepth; depth++ {
		g, found := index[id]
		if !found {
			break
		}
		names = append(names, g.Name)
		id = g.ParentID
	}
	slices.Reverse(names)
	return strings.Join(names, " / ")
}
