package layoutgraph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/lib/geo"
)

func TestRefinementBudgetStageAndTotalLimits(t *testing.T) {
	budget := NewRefinementBudget(10, 4)
	budget.BeginStage()
	budget.charge(3)
	require.False(t, budget.Exhausted())
	budget.charge(1)
	require.True(t, budget.Exhausted(), "stage limit reached")
	require.False(t, budget.TotalExhausted())

	budget.BeginStage()
	require.False(t, budget.Exhausted(), "a new stage gets a fresh stage budget")
	require.Equal(t, int64(4), budget.Used())
	budget.charge(-5)
	budget.charge(0)
	require.Equal(t, int64(4), budget.Used(), "non-positive charges are ignored")

	budget.charge(6)
	require.True(t, budget.Exhausted())
	require.True(t, budget.TotalExhausted())
	budget.BeginStage()
	require.True(t, budget.Exhausted(), "the aggregate limit outlives stages")

	budget.charge(1<<63 - 1)
	require.Equal(t, int64(1<<63-1), budget.Used(), "charges saturate")
}

func TestRefinementBudgetIsOptional(t *testing.T) {
	var budget *RefinementBudget
	budget.BeginStage()
	budget.charge(1)
	require.False(t, budget.Exhausted())
	require.False(t, budget.TotalExhausted())
	require.Zero(t, budget.Used())
	require.False(t, RefinementExhausted(context.Background()))
	require.False(t, RefinementExhausted(nil)) //nolint:staticcheck // nil contexts are tolerated

	zero := NewRefinementBudget(0, 0)
	zero.BeginStage()
	require.True(t, RefinementExhausted(ContextWithRefinementBudget(context.Background(), zero)))
}

func newRefinementTestGraph() (*Graph, *Node) {
	g := NewGraph()
	moved := NewNode(1, 10, 10)
	moved.TopLeft = geo.NewPoint(0, 0)
	g.AddNewNodeToContainer(nil, moved)
	other := NewNode(2, 10, 10)
	other.TopLeft = geo.NewPoint(100, 0)
	g.AddNewNodeToContainer(nil, other)
	return g, moved
}

// Commit charges the meter before any early exit, so accepted and rejected
// candidates are both metered from deterministic quantities only.
func TestCommitChargesRefinementBudget(t *testing.T) {
	g, moved := newRefinementTestGraph()
	budget := NewRefinementBudget(1_000, 1_000)
	budget.BeginStage()
	ctx := ContextWithRefinementBudget(context.Background(), budget)

	txn := mustNewTransaction(t, g, TransactionOptions{})
	txn.AddOp(func() error {
		moved.TopLeft.X = 20
		return nil
	})
	require.NoError(t, txn.Commit(ctx))
	accepted := budget.Used()
	// One commit, one operation and two validated nodes, plus the overlap
	// candidate references of the post-state sweep.
	require.Equal(t, int64(1+1+len(g.Nodes))+txn.overlapReferences, accepted)

	txn.Clear()
	require.NoError(t, txn.UpdateState())
	txn.AddOp(func() error { return ErrInvalidCandidate })
	require.ErrorIs(t, txn.Commit(ctx), ErrInvalidCandidate)
	require.Equal(t, accepted+int64(1+1+len(g.Nodes)), budget.Used(), "rejected candidates are metered")

	unmetered, unmeteredNode := newRefinementTestGraph()
	plain := mustNewTransaction(t, unmetered, TransactionOptions{})
	plain.AddOp(func() error {
		unmeteredNode.TopLeft.X = 20
		return nil
	})
	require.NoError(t, plain.Commit(context.Background()))
	require.Equal(t, accepted+int64(1+1+len(g.Nodes)), budget.Used(), "commits outside the metered context are free")
}

// The meter is advisory: an exhausted budget never rejects or fails a commit,
// so a stage can always re-commit a candidate it has already validated.
func TestExhaustedRefinementBudgetDoesNotRejectCommits(t *testing.T) {
	g, moved := newRefinementTestGraph()
	budget := NewRefinementBudget(1, 1)
	budget.BeginStage()
	budget.charge(1)
	ctx := ContextWithRefinementBudget(context.Background(), budget)
	require.True(t, RefinementExhausted(ctx))

	txn := mustNewTransaction(t, g, TransactionOptions{})
	txn.AddOp(func() error {
		moved.TopLeft.X = 30
		return nil
	})
	require.NoError(t, txn.Commit(ctx))
	require.Equal(t, 30.0, moved.TopLeft.X)
}
