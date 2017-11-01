package channelz

import (
	"sync"
	"time"

	"google.golang.org/grpc/connectivity"
)

type conn interface {
	Type() EntryType
}

type EntryType int

const (
	channelT = iota
	socketT
	serverT
)

type Channel interface {
	GetDesc() string
	GetState() connectivity.State
	GetTarget() string
	GetCallsStarted() int64
	GetLastCallStartedTime() time.Time
	GetCallsSucceeded() int64
	GetCallsFailed() int64
}

type channel struct {
	name string
	c    Channel
	mu   sync.Mutex
	// callsStarted        int64
	// callsSucceeded      int64
	// callsFailed         int64
	// lastCallStartedTime time.Time
	children map[int64]struct{}
}

func (*channel) Type() EntryType {
	return channelT
}

type Socket interface {
	GetDesc() string
	// SetIDs(int64, int64)
	GetMsgSent() int64
	GetMsgRecv() int64
	GetStreamsStarted() int64
	GetStreamsSucceeded() int64
	GetStreamsFailed() int64
	GetKpCount() int64
	GetLastStreamCreatedTime() time.Time
	GetLastMsgSentTime() time.Time
	GetLastMsgRecvTime() time.Time
	GetLocalFlowControlWindow() int64
	GetRemoteFlowControlWindow() int64
	IncrMsgSent()
	IncrMsgRecv()
}

type socket struct {
	name string
	s    Socket
}

func (*socket) Type() EntryType {
	return socketT
}

type Server interface {
	GetDesc() string
	GetCallsStarted() int64
	GetLastCallStartedTime() time.Time
	GetCallsSucceeded() int64
	GetCallsFailed() int64
}

type server struct {
	name string
	s    Server
}

func (*server) Type() EntryType {
	return serverT
}
