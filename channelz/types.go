package channelz

import (
	"sync"
	"time"

	"google.golang.org/grpc/connectivity"
)

type connKey struct{}

// var (
// 	// channelKey is the key used to store connection related data to context.
// 	channelKey connKey
// )

// type ChannelRef struct {
// 	ChannelID int64
// 	Name      string
// }

type ChannelCallCount interface {
	ParentCallStart()
	ParentCallSucceed()
	ParentCallFail()
}
type conn interface {
	IsChannel() bool
}

type Channel interface {
	GetDesc() string
	GetState() connectivity.State
}

type channel struct {
	name                string
	c                   Channel
	mu                  sync.Mutex
	callsStarted        int64
	callsSucceeded      int64
	callsFailed         int64
	lastCallStartedTime time.Time
	children            map[int64]struct{}
}

func (*channel) IsChannel() bool {
	return true
}

func (c *channel) CallStart() {
	c.mu.Lock()
	c.callsStarted++
	c.lastCallStartedTime = time.Now()
	c.mu.Unlock()
}

func (c *channel) CallSucceed() {
	c.mu.Lock()
	c.callsStarted--
	c.callsSucceeded++
	c.mu.Unlock()
}

func (c *channel) CallFail() {
	c.mu.Lock()
	c.callsStarted--
	c.callsFailed++
	c.mu.Unlock()
}

type Socket interface {
	GetDesc() string
	SetIDs(int64, int64)
}

type socket struct {
	name string
	s    Socket
}

func (*socket) IsChannel() bool {
	return false
}

// type ChannelData struct {
// 	cc                       *Channel
// 	State                    connectivity.State
// 	Target                   string
// 	CallsStarted             int64
// 	CallsSucceeded           int64
// 	CallsFailed              int64
// 	LastCallStartedTimestamp time.Time
// }
//
// type ChannelTrace struct {
// }
//
// type SocketRef struct {
// 	SocketID int64
// 	Name     string
// }
//
// type channel struct {
// 	Ref           ChannelRef
// 	Data          ChannelData
// 	Trace         ChannelTrace
// 	SubchannelRef []ChannelRef
// 	Socket        []SocketRef
// }
//
// type Socket struct {
// 	Ref        SocketRef
// 	Data       SocketData
// 	Local      Address
// 	Remote     Address
// 	Security   Security
// 	RemoteName string
// }
//
// type SocketData struct {
// 	StreamsStarted                   int64
// 	StreamsSucceeded                 int64
// 	StreamsFailed                    int64
// 	MessageSent                      int64
// 	MessageReceived                  int64
// 	KeepAlivesSent                   int64
// 	LastLocalStreamCreatedTimestamp  time.Time
// 	LastRemoteStreamCreatedTimestamp time.Time
// 	LastMessageSentTimestamp         time.Time
// 	LastMessageReceivedTimestamp     time.Time
// 	LocalFlowControlWindow           int64
// 	RemoteFlowControlWindow          int64
// 	Option                           []SocketOption
// }
//
// type Address struct {
// 	IPAddress []byte
// 	port      int32
// }
//
// type Security struct {
// 	KeyExchanged      string
// 	Cipher            string
// 	LocalCertificate  []byte
// 	RemoteCertificate []byte
// }
//
// type SocketOption struct {
// 	Name  string
// 	Value string
// }
//
// type SocketOptionTimeout struct {
// 	Duration time.Duration
// }
//
// type SocketOptionLinger struct {
// 	Duration time.Duration
// }
//
// type SocketOptionTcpInfo struct {
// }
