package handlers

import (
	"fmt"
	"sync/atomic"
	"time"
)

var commandSequence atomic.Uint64

// newCommandID 在单进程和高并发下都保持可追踪且不重复。
func newCommandID(prefix string) string {
	sequence := commandSequence.Add(1)
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), sequence)
}
