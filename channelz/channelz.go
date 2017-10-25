package channelz

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/grpclog"
)

func init() {
	channelTbl = &channelMap{
		m: make(map[int64]conn),
	}
	idGen = idGenerator{}

	go func() {
		for i := 0; i < 15; i++ {
			time.Sleep(time.Second)
			fmt.Printf("######## %+v\n", channelTbl)
			for k, v := range channelTbl.m {
				// fmt.Printf("##  %+v, %+v\n", k, v)
				if v.IsChannel() {
					fmt.Println("unique id:", k, "This is a channel. Info listed below")
					fmt.Println("Connectivity state:", v.(*channel).c.GetState())
					fmt.Println("Target:", v.(*channel).c.GetTarget())
					fmt.Println("Calls started:", v.(*channel).callsStarted)
					fmt.Println("Calls succeeded:", v.(*channel).callsSucceeded)
					fmt.Println("Calls failed:", v.(*channel).callsFailed)
					fmt.Println("Last call started time:", v.(*channel).lastCallStartedTime.String())
					fmt.Printf("%+v\n", v)
				}
				if !v.IsChannel() {
					fmt.Println("unique id:", k, "This is a socket. Info listed below")
					fmt.Println("Streams started:", v.(*socket).s.GetStreamsStarted())
					fmt.Println("Streams succeeded:", v.(*socket).s.GetStreamsSucceeded())
					fmt.Println("Streams failed:", v.(*socket).s.GetStreamsFailed())
					fmt.Println("Msg sent:", v.(*socket).s.GetMsgSent())
					fmt.Println("Msg recv:", v.(*socket).s.GetMsgRecv())
					fmt.Println("Keepalives sent:", v.(*socket).s.GetKpCount())
					fmt.Println("Last local stream created:", v.(*socket).s.GetLastStreamCreatedTime())
					fmt.Println("Last msg sent:", v.(*socket).s.GetLastMsgSentTime())
					fmt.Println("Last msg recv:", v.(*socket).s.GetLastMsgRecvTime())
					fmt.Println("Local flow fontrol window:", v.(*socket).s.GetLocalFlowControlWindow())
					fmt.Println("Remote flow control window:", v.(*socket).s.GetRemoteFlowControlWindow())
					fmt.Printf("%+v\n", v)
				}
			}
			fmt.Println("\n\n")
		}
	}()
}

type channelMap struct {
	mu sync.Mutex
	m  map[int64]conn
}

func (c *channelMap) Add(id int64, cn conn) {
	c.mu.Lock()
	c.m[id] = cn
	c.mu.Unlock()
}

func (c *channelMap) Delete(id int64) {
	c.mu.Lock()
	delete(c.m, id)
	c.mu.Unlock()
}

func (c *channelMap) Value(id int64) (cn conn, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cn, ok = c.m[id]
	return
}

func (c *channelMap) Lock() {
	c.mu.Lock()
}

func (c *channelMap) Unlock() {
	c.mu.Unlock()
}

var (
	channelTbl       *channelMap
	topLevelChannels []int64
	idGen            idGenerator
)

func RegisterTopChannel(c Channel) int64 {
	id := RegisterChannel(c)
	//TODO: locking?
	topLevelChannels = append(topLevelChannels, id)
	return id
}

func RegisterChannel(c Channel) int64 {
	id := idGen.genID()
	channelTbl.Add(id, &channel{name: c.GetDesc(), c: c, children: make(map[int64]struct{})})
	return id
}

func RegisterSocket(pid int64, s Socket) int64 {
	id := idGen.genID()
	channelTbl.Add(id, &socket{name: s.GetDesc(), s: s})
	s.SetIDs(pid, id)
	return id
}

func RemoveEntry(id int64) {
	channelTbl.Delete(id)
}

func AddChild(pid, cid int64) {
	channelTbl.Lock()
	defer channelTbl.Unlock()
	c, ok := channelTbl.m[pid]
	if !ok {
		grpclog.Infof("parent has been deleted.")
		return
	}
	if c.IsChannel() {
		c.(*channel).children[cid] = struct{}{}
	} else {
		grpclog.Error("socket cannot have children")
	}
}

func RemoveChild(pid, cid int64) {
	channelTbl.Lock()
	defer channelTbl.Unlock()

	c, ok := channelTbl.m[pid]
	if !ok {
		grpclog.Info("parent has been deleted.")
		return
	}
	if c.IsChannel() {
		delete(c.(*channel).children, cid)
	} else {
		grpclog.Error("socket cannot have children")
	}
}

func CallStart(id int64) {
	channelTbl.Lock()
	defer channelTbl.Unlock()
	c, ok := channelTbl.m[id]
	if !ok {
		grpclog.Infof("no such channel with id: %d currently exists.", id)
		return
	}
	if c.IsChannel() {
		c.(*channel).CallStart()
	} else {
		grpclog.Error("socket do not have such function")
	}
}

func CallSucceed(id int64) {
	channelTbl.Lock()
	defer channelTbl.Unlock()
	c, ok := channelTbl.m[id]
	if !ok {
		grpclog.Infof("no such channel with id: %d currently exists.", id)
		return
	}
	if c.IsChannel() {
		c.(*channel).CallSucceed()
	} else {
		grpclog.Error("socket do not have such function")
	}
}

func CallFail(id int64) {
	channelTbl.Lock()
	defer channelTbl.Unlock()
	c, ok := channelTbl.m[id]
	if !ok {
		grpclog.Infof("no such channel with id: %d currently exists.", id)
		return
	}
	if c.IsChannel() {
		c.(*channel).CallFail()
	} else {
		grpclog.Error("socket do not have such function")
	}
}

type idGenerator struct {
	id int64
}

func (i *idGenerator) genID() int64 {
	return atomic.AddInt64(&i.id, 1)
}

type counter struct {
	c int64
}

func (c *counter) incr() {
	atomic.AddInt64(&c.c, 1)
}

func (c *counter) decr() {
	atomic.AddInt64(&c.c, -1)
}

func (c *counter) counter() int {
	return int(atomic.LoadInt64(&c.c))
}
