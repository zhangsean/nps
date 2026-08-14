package rate

import (
	"testing"
	"time"
)

func TestGetConsumesLargeRequestInChunks(t *testing.T) {
	r := NewRate(10)
	r.ReturnBucket(20)

	done := make(chan struct{})
	go func() {
		r.Get(25)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Get returned before enough tokens were available")
	case <-time.After(50 * time.Millisecond):
	}

	r.ReturnBucket(10)

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Get did not consume a request larger than the bucket size in chunks")
	}
}

func TestGetWithInvalidRateReturns(t *testing.T) {
	r := NewRate(0)

	done := make(chan struct{})
	go func() {
		r.Get(1)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Get blocked for an invalid zero rate")
	}
}
