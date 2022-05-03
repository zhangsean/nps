package sheap_test

import (
	"container/heap"
	"ehang.io/nps/lib/sheap"
	"github.com/stretchr/testify/assert"
	"testing"
)

type TestElement struct {
	weight  int
	payload interface{}
}

func (it *TestElement) Weight() int64 {
	return int64(it.weight)
}

func TestTest(t *testing.T) {
	h := &sheap.Heap{}

	heap.Push(h, &TestElement{1, "1"})
	heap.Push(h, &TestElement{3, "3"})
	heap.Push(h, &TestElement{2, "2"})
	heap.Push(h, &TestElement{1, "1"})
	heap.Push(h, &TestElement{1, "100"})

	assert.Equal(t, 1, heap.Pop(h).(*TestElement).weight)
	assert.Equal(t, 1, heap.Pop(h).(*TestElement).weight)
	assert.Equal(t, 1, heap.Pop(h).(*TestElement).weight)
	assert.Equal(t, 2, heap.Pop(h).(*TestElement).weight)
	assert.Equal(t, 3, heap.Pop(h).(*TestElement).weight)
	assert.Equal(t, 0, h.Len())
}
