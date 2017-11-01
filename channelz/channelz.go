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
		m:                make(map[int64]conn),
		topLevelChannels: make(map[int64]struct{}),
	}
	idGen = idGenerator{}

	go func() {
		for i := 0; i < 15; i++ {
			time.Sleep(time.Second)
			fmt.Printf("######## %+v\n", channelTbl)
			for k, v := range channelTbl.m {
				// fmt.Printf("##  %+v, %+v\n", k, v)
				fmt.Println("******************************************************")
				if v.Type() == channelT {
					fmt.Println("unique id:", k, "This is a channel. Info listed below")
					fmt.Println("Connectivity state:", v.(*channel).c.GetState())
					fmt.Println("Target:", v.(*channel).c.GetTarget())
					// fmt.Println("Calls started:", v.(*channel).callsStarted)
					fmt.Println("NEW** Calls started:", v.(*channel).c.GetCallsStarted())
					// fmt.Println("Calls succeeded:", v.(*channel).callsSucceeded)
					fmt.Println("NEW** Calls succeeded:", v.(*channel).c.GetCallsSucceeded())
					// fmt.Println("Calls failed:", v.(*channel).callsFailed)
					fmt.Println("NEW** Calls failed:", v.(*channel).c.GetCallsFailed())
					// fmt.Println("Last call started time:", v.(*channel).lastCallStartedTime.String())
					fmt.Println("NEW** Last call started time:", v.(*channel).c.GetLastCallStartedTime().String())
					fmt.Printf("%+v\n", v)
				} else if v.Type() == socketT {
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
				} else {
					fmt.Println("unique id:", k, "This is a server. Info listed below")
					fmt.Println("Calls started:", v.(*server).s.GetCallsStarted())
					fmt.Println("Calls succeeded:", v.(*server).s.GetCallsSucceeded())
					fmt.Println("Calls failed:", v.(*server).s.GetCallsFailed())
					fmt.Println("Last call started time:", v.(*server).s.GetLastCallStartedTime().String())
					fmt.Printf("%+v\n", v)
				}
			}
			fmt.Println("\n\n")
		}
	}()
}

type channelMap struct {
	mu               sync.Mutex
	m                map[int64]conn
	topLevelChannels map[int64]struct{}
}

func (c *channelMap) Add(id int64, cn conn) {
	c.mu.Lock()
	c.m[id] = cn
	c.mu.Unlock()
}

func (c *channelMap) AddTopChannel(id int64, cn conn) {
	c.mu.Lock()
	c.m[id] = cn
	c.topLevelChannels[id] = struct{}{}
	c.mu.Unlock()
}

func (c *channelMap) Delete(id int64) {
	c.mu.Lock()
	delete(c.m, id)
	if _, ok := c.topLevelChannels[id]; ok {
		delete(c.topLevelChannels, id)
	}
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

func (c *channelMap) GetTopChannels() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	res := make([]int64, 0, len(c.topLevelChannels))
	for k := range c.topLevelChannels {
		res = append(res, k)
	}
	return res
}

var (
	channelTbl *channelMap
	idGen      idGenerator
)

func RegisterTopChannel(c Channel) int64 {
	id := idGen.genID()
	channelTbl.AddTopChannel(id, &channel{name: c.GetDesc(), c: c, children: make(map[int64]struct{})})
	return id
}

func RegisterChannel(c Channel) int64 {
	id := idGen.genID()
	channelTbl.Add(id, &channel{name: c.GetDesc(), c: c, children: make(map[int64]struct{})})
	return id
}

func RegisterSocket(s Socket) int64 {
	id := idGen.genID()
	channelTbl.Add(id, &socket{name: s.GetDesc(), s: s})
	return id
}

func RegisterServer(s Server) int64 {
	id := idGen.genID()
	channelTbl.Add(id, &server{name: s.GetDesc(), s: s})
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
	if c.Type() == channelT {
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
	if c.Type() == channelT {
		delete(c.(*channel).children, cid)
	} else {
		grpclog.Error("socket cannot have children")
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

func GetTopChannels() []int64 {
	return channelTbl.GetTopChannels()
}
