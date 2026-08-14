package rate

import (
	"sync/atomic"
	"time"
)

type Rate struct {
	bucketSize        int64
	bucketSurplusSize int64
	bucketAddSize     int64
	stopChan          chan bool
	NowRate           int64
}

func NewRate(addSize int64) *Rate {
	return &Rate{
		bucketSize:        addSize * 2,
		bucketSurplusSize: 0,
		bucketAddSize:     addSize,
		stopChan:          make(chan bool),
	}
}

func (s *Rate) Start() {
	go s.session()
}

func (s *Rate) add(size int64) {
	if size <= 0 || s.bucketSize <= 0 {
		return
	}
	for {
		current := atomic.LoadInt64(&s.bucketSurplusSize)
		available := s.bucketSize - current
		if available <= 0 {
			return
		}
		addSize := size
		if addSize > available {
			addSize = available
		}
		if atomic.CompareAndSwapInt64(&s.bucketSurplusSize, current, current+addSize) {
			return
		}
	}
}

// 回桶
func (s *Rate) ReturnBucket(size int64) {
	s.add(size)
}

// 停止
func (s *Rate) Stop() {
	s.stopChan <- true
}

func (s *Rate) Get(size int64) {
	if size <= 0 || s.bucketSize <= 0 || s.bucketAddSize <= 0 {
		return
	}
	for size > s.bucketSize {
		s.get(s.bucketSize)
		size -= s.bucketSize
	}
	s.get(size)
}

func (s *Rate) get(size int64) {
	if size <= 0 {
		return
	}
	for {
		current := atomic.LoadInt64(&s.bucketSurplusSize)
		if current >= size && atomic.CompareAndSwapInt64(&s.bucketSurplusSize, current, current-size) {
			return
		}
		break
	}
	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			current := atomic.LoadInt64(&s.bucketSurplusSize)
			if current >= size && atomic.CompareAndSwapInt64(&s.bucketSurplusSize, current, current-size) {
				return
			}
		}
	}
}

func (s *Rate) session() {
	ticker := time.NewTicker(time.Second * 1)
	for {
		select {
		case <-ticker.C:
			if rs := s.bucketAddSize - s.bucketSurplusSize; rs > 0 {
				s.NowRate = rs
			} else {
				s.NowRate = s.bucketSize - s.bucketSurplusSize
			}
			s.add(s.bucketAddSize)
		case <-s.stopChan:
			ticker.Stop()
			return
		}
	}
}
