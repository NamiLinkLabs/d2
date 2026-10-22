package d2isometric

import (
	"github.com/d2lang/d2/d2layouts/d2sequence"
	"github.com/d2lang/d2/d2target"
)

// A sequence scope is an explicit source container, or the source root (-1).
// The exporter omits the root's sequence type in many compiled diagrams, so
// an exact synthetic lifeline also confirms that scope. Z indices alone do
// not classify unrelated shapes in a mixed ordinary/sequence diagram.
func classifySequence(scene *Scene, indices map[string]int, parents []int) {
	confirmed := map[int]bool{-1: scene.Root.Type == d2target.ShapeSequenceDiagram}
	scopes := make([]int, len(scene.Nodes))
	for i := range scene.Nodes {
		scopes[i] = -1
		for p := parents[i]; p >= 0; p = parents[p] {
			if scene.Nodes[p].Type == d2target.ShapeSequenceDiagram {
				scopes[i] = p
				break
			}
		}
		if scene.Nodes[i].Type == d2target.ShapeSequenceDiagram {
			scene.Nodes[i].SequenceRole = "container"
			confirmed[i] = true
		}
	}
	isNode := func(id string) bool {
		_, exists := indices[id]
		return exists
	}
	for i := range scene.Edges {
		edge := &scene.Edges[i]
		// BuildScene copies Source, Target and the arrowheads from Original.
		if !d2sequence.IsLifeline(edge.Metadata.Original, isNode) {
			continue
		}
		actor := indices[edge.Source]
		edge.SequenceRole = "lifeline"
		scene.Nodes[actor].SequenceRole = "actor"
		confirmed[scopes[actor]] = true
	}
	for _, exists := range confirmed {
		scene.HasSequence = scene.HasSequence || exists
	}
	if !scene.HasSequence {
		return
	}
	for i := range scene.Nodes {
		node := &scene.Nodes[i]
		if node.SequenceRole != "" || !confirmed[scopes[i]] {
			continue
		}
		switch node.Metadata.Original.ZIndex {
		case d2sequence.SPAN_Z_INDEX:
			node.SequenceRole = "span"
		case d2sequence.GROUP_Z_INDEX:
			if node.Metadata.Original.Blend {
				node.SequenceRole = "group"
			}
		case d2sequence.NOTE_Z_INDEX:
			node.SequenceRole = "note"
		}
	}
	for i := range scene.Edges {
		edge := &scene.Edges[i]
		source, hasSource := indices[edge.Source]
		target, hasTarget := indices[edge.Target]
		if edge.SequenceRole == "" && edge.Metadata.Original.ZIndex == d2sequence.MESSAGE_Z_INDEX &&
			hasSource && hasTarget && scopes[source] == scopes[target] && confirmed[scopes[source]] {
			edge.SequenceRole = "message"
		}
	}
	// ParentID remains the authored hierarchy. A span or note is a timeline
	// annotation of an actor, not evidence that the actor encloses its box.
	// Real ordinary children still turn their actor into a physical container.
	for i := range scene.Nodes {
		scene.Nodes[i].Container = scene.Nodes[i].SequenceRole == "container"
	}
	for i := range scene.Nodes {
		if sequenceSemanticChild(scene.Nodes[i].SequenceRole) {
			continue
		}
		parent := parents[i]
		for parent >= 0 && sequenceSemanticChild(scene.Nodes[parent].SequenceRole) {
			parent = parents[parent]
		}
		if parent >= 0 {
			scene.Nodes[parent].Container = true
		}
	}
}

func sequenceSemanticChild(role string) bool {
	return role == "span" || role == "note" || role == "group"
}
