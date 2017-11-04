package channelz

import (
	"net"
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

type ChannelMetric struct {
	RefName                  string
	State                    connectivity.State
	Target                   string
	CallsStarted             int64
	CallsSucceeded           int64
	CallsFailed              int64
	LastCallStartedTimestamp time.Time
	// trace
}

type Channel interface {
	ChannelzMetrics() ChannelMetric
}

type channel struct {
	c        Channel
	mu       sync.Mutex
	children map[int64]struct{}
}

func (*channel) Type() EntryType {
	return channelT
}

type SocketMetric struct {
	RefName                          string
	StreamsStarted                   int64
	StreamsSucceeded                 int64
	StreamsFailed                    int64
	MessagesSent                     int64
	MessagesReceived                 int64
	KeepAlivesSent                   int64
	LastLocalStreamCreatedTimestamp  time.Time
	LastRemoteStreamCreatedTimestamp time.Time
	LastMessageSentTimestamp         time.Time
	LastMessageReceivedTimestamp     time.Time
	LocalFlowControlWindow           int64
	RemoteFlowControlWindow          int64
	//socket options
	Local  net.Addr
	Remote net.Addr
	// Security
	RemoteName string
}

type Socket interface {
	ChannelzMetrics() SocketMetric
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

type ServerMetric struct {
	RefName                  string
	CallsStarted             int64
	CallsSucceeded           int64
	CallsFailed              int64
	LastCallStartedTimestamp time.Time
	// trace
}

type Server interface {
	ChannelzMetrics() ServerMetric
}

type server struct {
	name string
	s    Server
}

func (*server) Type() EntryType {
	return serverT
}
