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
		servers:          make(map[int64]struct{}),
		orphans:          make(map[int64]struct{}),
	}
	idGen = idGenerator{}

	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(time.Second)
			fmt.Printf("######## %+v\n", channelTbl)
			for _, v := range channelTbl.m {
				// fmt.Printf("##  %+v, %+v\n", k, v)
				fmt.Println("******************************************************")
				if v.Type() == channelT {
					// fmt.Println("unique id:", k, "This is a channel. Info listed below")
					// fmt.Printf("%#+v\n", v.(*channel).c.ChannelzMetrics())
					// fmt.Printf("children: %+v\n", v.(*channel).children)
				} else if v.Type() == socketT {
					// fmt.Println("unique id:", k, "This is a socket. Info listed below")
					// fmt.Printf("%#+v\n", v.(*socket).s.ChannelzMetrics())
				} else {
					// fmt.Println("unique id:", k, "This is a server. Info listed below")
					// fmt.Printf("%#+v\n", v.(*server).s.ChannelzMetrics())
					// fmt.Printf("children: %+v\n", v.(*server).children)
				}
			}
			fmt.Println("\n\n")
		}
	}()
}

type channelMap struct {
	mu               sync.RWMutex
	m                map[int64]conn
	topLevelChannels map[int64]struct{}
	servers          map[int64]struct{}
	orphans          map[int64]struct{}
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

func (c *channelMap) AddServer(id int64, cn conn) {
	c.mu.Lock()
	c.m[id] = cn
	c.servers[id] = struct{}{}
	c.mu.Unlock()
}

func (c *channelMap) AddOrphan(id int64) {
	c.mu.Lock()
	c.orphans[id] = struct{}{}
	c.mu.Unlock()
}

func (c *channelMap) removeChild(pid, cid int64) {
	e, ok := c.m[pid]
	if !ok {
		grpclog.Info("parent has been deleted.")
		return
	}
	switch e.Type() {
	case channelT:
		delete(e.(*channel).children, cid)
	case serverT:
		delete(e.(*server).children, cid)
	case socketT:
		grpclog.Error("socket cannot have children")
	}
}

func (c *channelMap) Delete(id int64) {
	c.mu.Lock()
	if _, ok := c.topLevelChannels[id]; ok {
		delete(c.topLevelChannels, id)
		if v, ok := c.m[id]; ok {
			if v.(*channel).children != nil && len(v.(*channel).children) > 0 {
				for k := range v.(*channel).children {
					c.orphans[k] = struct{}{}
				}
			}
		} else {
			grpclog.Errorf("Deleting an entity that doesn't exisit, id: %d", id)
		}
	}

	if _, ok := c.servers[id]; ok {
		delete(c.servers, id)
		if v, ok := c.m[id]; ok {
			if v.(*server).children != nil && len(v.(*server).children) > 0 {
				for k := range v.(*server).children {
					c.orphans[k] = struct{}{}
				}
			}
		} else {
			grpclog.Errorf("Deleting an entity that doesn't exisit, id: %d", id)
		}
	}

	if _, ok := c.orphans[id]; ok {
		delete(c.orphans, id)
	}

	if v, ok := c.m[id]; ok {
		switch v.Type() {
		case socketT:
			c.removeChild(v.(*socket).pid, id)
		case channelT:
			if v.(*channel).pid != 0 {
				c.removeChild(v.(*channel).pid, id)
			}
		default:
		}
	}
	delete(c.m, id)
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
	channelTbl.AddTopChannel(id, &channel{c: c, children: make(map[int64]string)})
	return id
}

func RegisterChannel(c Channel) int64 {
	id := idGen.genID()
	channelTbl.Add(id, &channel{c: c, children: make(map[int64]string)})
	return id
}

func RegisterSocket(s Socket) int64 {
	id := idGen.genID()
	s.SetID(id)
	channelTbl.Add(id, &socket{s: s})
	return id
}

func RegisterServer(s Server) int64 {
	id := idGen.genID()
	channelTbl.AddServer(id, &server{s: s, children: make(map[int64]string)})
	return id
}

func RemoveEntry(id int64) {
	channelTbl.Delete(id)
}

func AddChild(pid, cid int64, ref string) {
	channelTbl.mu.RLock()
	defer channelTbl.mu.RUnlock()

	// Add to parent's children set
	p, ok := channelTbl.m[pid]
	if !ok {
		grpclog.Infof("parent has been deleted, id %d", pid)
		return
	}
	p.Lock()
	switch p.Type() {
	case channelT:
		p.(*channel).children[cid] = ref
	case serverT:
		p.(*server).children[cid] = ref
	case socketT:
		grpclog.Errorf("socket cannot have children, id: %d", pid)
		return
	}
	p.Unlock()

	// Assign parent to child's parent
	//TODO: should we delete child from parent children set if we found the child doesn't exist?
	c, ok := channelTbl.m[cid]
	if !ok {
		grpclog.Infof("children does not exisit, id %d", cid)
		return
	}
	c.Lock()
	switch c.Type() {
	case channelT:
		channelTbl.m[cid].(*channel).pid = pid
	case socketT:
		channelTbl.m[cid].(*socket).pid = pid
	case serverT:
		grpclog.Infof("server cannot be a child, id: %d", cid)
	}
	c.Unlock()
}

type idGenerator struct {
	id int64
}

func (i *idGenerator) genID() int64 {
	return atomic.AddInt64(&i.id, 1)
}
