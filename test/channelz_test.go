/*
 *
 * Copyright 2018 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package test

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/channelz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/test/leakcheck"
)

func (te *test) startServers(ts testpb.TestServiceServer, num int) {
	for i := 0; i < num; i++ {
		te.startServer(ts)
		te.srvs = append(te.srvs, te.srv)
		te.srvAddrs = append(te.srvAddrs, te.srvAddr)
		te.srv = nil
		te.srvAddr = ""
	}
}

func turnOnChannelzAndClearPreviousChannelzData() {
	grpc.RegisterChannelz()
	channelz.NewChannelzStorage()
}

func TestCZServerRegistrationAndDeletion(t *testing.T) {
	defer leakcheck.Check(t)
	testcases := []struct {
		total  int
		start  int64
		length int
		end    bool
	}{
		{total: channelz.EntryPerPage, start: 0, length: channelz.EntryPerPage, end: true},
		{total: channelz.EntryPerPage + 1, start: 0, length: channelz.EntryPerPage, end: false},
		{total: channelz.EntryPerPage + 1, start: int64(2*(channelz.EntryPerPage+1) + 1), length: 0, end: true},
	}

	for _, c := range testcases {
		turnOnChannelzAndClearPreviousChannelzData()
		e := tcpClearRREnv
		te := newTest(t, e)
		te.startServers(&testServer{security: e.security}, c.total)

		ss, end := channelz.GetServers(c.start)
		if len(ss) != c.length || end != c.end {
			t.Fatalf("GetServers(%d) = %+v (len of which: %d), end: %+v, want len(GetServers(%d)) = %d, end: %+v", c.start, ss, len(ss), end, c.start, c.length, c.end)
		}
		te.tearDown()
		ss, end = channelz.GetServers(c.start)
		if len(ss) != 0 || !end {
			t.Fatalf("GetServers(0) = %+v (len of which: %d), end: %+v, want len(GetServers(0)) = 0, end: true", ss, len(ss), end)
		}
	}
}

func TestCZTopChannelRegistrationAndDeletion(t *testing.T) {
	defer leakcheck.Check(t)
	testcases := []struct {
		total  int
		start  int64
		length int
		end    bool
	}{
		{total: channelz.EntryPerPage + 1, start: 0, length: channelz.EntryPerPage, end: false},
		{total: channelz.EntryPerPage + 1, start: int64(2*(channelz.EntryPerPage+1) + 1), length: 0, end: true},
	}

	for _, c := range testcases {
		turnOnChannelzAndClearPreviousChannelzData()
		e := tcpClearRREnv
		te := newTest(t, e)
		var ccs []*grpc.ClientConn
		for i := 0; i < c.total; i++ {
			cc := te.clientConn()
			te.cc = nil
			// avoid making next dial blocking
			te.srvAddr = ""
			ccs = append(ccs, cc)
		}
		time.Sleep(10 * time.Millisecond)
		tcs, end := channelz.GetTopChannels(c.start)
		if len(tcs) != c.length || end != c.end {
			t.Fatalf("GetTopChannels(%d) = %+v (len of which: %d), end: %+v, want len(GetTopChannels(%d)) = %d, end: %+v", c.start, tcs, len(tcs), end, c.start, c.length, c.end)
		}
		for _, cc := range ccs {
			cc.Close()
		}
		tcs, end = channelz.GetTopChannels(c.start)
		if len(tcs) != 0 || !end {
			t.Fatalf("GetTopChannels(0) = %+v (len of which: %d), end: %+v, want len(GetTopChannels(0)) = 0, end: true", tcs, len(tcs), end)
		}

		te.tearDown()
	}
}

func TestCZNestedChannelRegistrationAndDeletion(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	// avoid calling API to set balancer type, which will void service config's change of balancer.
	e.balancer = ""
	te := newTest(t, e)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()
	resolvedAddrs := []resolver.Address{{Addr: "127.0.0.1:0", Type: resolver.GRPCLB, ServerName: "grpclb.server"}}
	r.InitialAddrs(resolvedAddrs)
	te.resolverScheme = r.Scheme()
	te.clientConn()
	defer te.tearDown()
	time.Sleep(10 * time.Millisecond)
	tcs, _ := channelz.GetTopChannels(0)
	if len(tcs) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tcs))
	}
	if len(tcs[0].NestedChans) != 1 {
		t.Fatalf("There should be one nested channel from grpclb, not %d", len(tcs[0].NestedChans))
	}

	r.NewServiceConfig(`{"loadBalancingPolicy": "round_robin"}`)
	r.NewAddress([]resolver.Address{{Addr: "127.0.0.1:0"}})

	// wait for the shutdown of grpclb balancer
	time.Sleep(10 * time.Millisecond)
	tcs, _ = channelz.GetTopChannels(0)
	if len(tcs) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tcs))
	}
	if len(tcs[0].NestedChans) != 0 {
		t.Fatalf("There should be 0 nested channel from grpclb, not %d", len(tcs[0].NestedChans))
	}

}

func TestCZClientSubChannelSocketRegistrationAndDeletion(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	num := 3 // number of backends
	te := newTest(t, e)
	var svrAddrs []resolver.Address
	te.startServers(&testServer{security: e.security}, num)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()
	for _, a := range te.srvAddrs {
		svrAddrs = append(svrAddrs, resolver.Address{Addr: a})
	}
	r.InitialAddrs(svrAddrs)
	te.resolverScheme = r.Scheme()
	te.clientConn()
	defer te.tearDown()
	// Here, we just wait for all sockets to be up. In the future, if we implement
	// IDLE, we may need to make several rpc calls to create the sockets.
	time.Sleep(100 * time.Millisecond)
	tcs, _ := channelz.GetTopChannels(0)
	if len(tcs) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tcs))
	}
	if len(tcs[0].SubChans) != num {
		t.Fatalf("There should be %d subchannel not %d", num, len(tcs[0].SubChans))
	}
	count := 0
	for k := range tcs[0].SubChans {
		sc := channelz.GetSubChannel(k)
		if sc == nil {
			t.Fatalf("got <nil> subchannel")
		}
		count += len(sc.Sockets)
	}
	if count != num {
		t.Fatalf("There should be %d sockets not %d", num, count)
	}

	r.NewAddress(svrAddrs[:len(svrAddrs)-1])
	time.Sleep(100 * time.Millisecond)
	tcs, _ = channelz.GetTopChannels(0)
	if len(tcs[0].SubChans) != num-1 {
		t.Fatalf("There should be %d subchannel not %d", num-1, len(tcs[0].SubChans))
	}
	count = 0
	for k := range tcs[0].SubChans {
		sc := channelz.GetSubChannel(k)
		if sc == nil {
			t.Fatalf("got <nil> subchannel")
		}
		count += len(sc.Sockets)
	}
	if count != num-1 {
		t.Fatalf("There should be %d sockets not %d", num-1, count)
	}
}

func TestCZServerSocketRegistrationAndDeletion(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	num := 3 // number of clients
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	var ccs []*grpc.ClientConn
	for i := 0; i < num; i++ {
		cc := te.clientConn()
		te.cc = nil
		ccs = append(ccs, cc)
	}
	defer func() {
		for _, c := range ccs[:len(ccs)-1] {
			c.Close()
		}
	}()
	time.Sleep(10 * time.Millisecond)
	ss, _ := channelz.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should only be one server, not %d", len(ss))
	}
	if len(ss[0].ListenSockets) != 1 {
		t.Fatalf("There should only be one server listen socket, not %d", len(ss[0].ListenSockets))
	}
	ns, _ := channelz.GetServerSockets(ss[0].ID, 0)
	if len(ns) != num {
		t.Fatalf("There should be %d normal sockets not %d", num, len(ns))
	}

	ccs[len(ccs)-1].Close()
	time.Sleep(10 * time.Millisecond)
	ns, _ = channelz.GetServerSockets(ss[0].ID, 0)
	if len(ns) != num-1 {
		t.Fatalf("There should be %d normal sockets not %d", num-1, len(ns))
	}
}

func TestCZServerListenSocketDeletion(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	go s.Serve(lis)
	time.Sleep(10 * time.Millisecond)
	ss, _ := channelz.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should only be one server, not %d", len(ss))
	}
	if len(ss[0].ListenSockets) != 1 {
		t.Fatalf("There should only be one server listen socket, not %d", len(ss[0].ListenSockets))
	}

	lis.Close()
	time.Sleep(10 * time.Millisecond)
	ss, _ = channelz.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should only be one server, not %d", len(ss))
	}
	if len(ss[0].ListenSockets) != 0 {
		t.Fatalf("There should only be 0 server listen socket, not %d", len(ss[0].ListenSockets))
	}
	s.Stop()
}

type dummyChannel struct {
	channelzID int64
}

func (d *dummyChannel) ChannelzMetric() *channelz.ChannelMetric {
	return &channelz.ChannelMetric{}
}

func (d *dummyChannel) SetChannelzID(id int64) {
	d.channelzID = id
}

type dummySocket struct {
	channelzID int64
}

func (d *dummySocket) ChannelzMetric() *channelz.SocketMetric {
	return &channelz.SocketMetric{}
}

func (d *dummySocket) SetChannelzID(id int64) {
	d.channelzID = id
}

func TestCZRecusivelyDeletionOfEntry(t *testing.T) {
	//           +--+TopChan+---+
	//           |              |
	//           v              v
	//    +-+SubChan1+--+   SubChan2
	//    |             |
	//    v             v
	// Socket1       Socket2
	channelz.NewChannelzStorage()
	var topChan, subChan1, subChan2 dummyChannel
	var skt1, skt2 dummySocket
	channelz.RegisterChannel(&topChan, channelz.TopChannelT, 0, "")
	channelz.RegisterChannel(&subChan1, channelz.SubChannelT, topChan.channelzID, "")
	channelz.RegisterChannel(&subChan2, channelz.SubChannelT, topChan.channelzID, "")
	channelz.RegisterSocket(&skt1, channelz.NormalSocketT, subChan1.channelzID, "")
	channelz.RegisterSocket(&skt2, channelz.NormalSocketT, subChan1.channelzID, "")

	tcs, _ := channelz.GetTopChannels(0)
	if tcs == nil || len(tcs) != 1 {
		t.Fatalf("There should be one TopChannel entry")
	}
	if len(tcs[0].SubChans) != 2 {
		t.Fatalf("There should be two SubChannel entries")
	}
	sc := channelz.GetSubChannel(subChan1.channelzID)
	if sc == nil || len(sc.Sockets) != 2 {
		t.Fatalf("There should be two Socket entries")
	}

	channelz.RemoveEntry(topChan.channelzID)
	tcs, _ = channelz.GetTopChannels(0)
	if tcs == nil || len(tcs) != 1 {
		t.Fatalf("There should be one TopChannel entry")
	}

	channelz.RemoveEntry(subChan1.channelzID)
	channelz.RemoveEntry(subChan2.channelzID)
	tcs, _ = channelz.GetTopChannels(0)
	if tcs == nil || len(tcs) != 1 {
		t.Fatalf("There should be one TopChannel entry")
	}
	if len(tcs[0].SubChans) != 1 {
		t.Fatalf("There should be one SubChannel entry")
	}

	channelz.RemoveEntry(skt1.channelzID)
	channelz.RemoveEntry(skt2.channelzID)
	tcs, _ = channelz.GetTopChannels(0)
	if tcs != nil {
		t.Fatalf("There should be no TopChannel entry")
	}
}

func prettyPrintChannelMetric(c *channelz.ChannelMetric) {
	if c == nil {
		return
	}
	fmt.Println("{")
	fmt.Printf("  ID: %+v\n", c.ID)
	fmt.Printf("  RefName: %+v\n", c.RefName)
	fmt.Printf("  State: %+v\n", c.State)
	fmt.Printf("  Target: %+v\n", c.Target)
	fmt.Printf("  CallsStarted: %+v\n", c.CallsStarted)
	fmt.Printf("  CallsSucceeded: %+v\n", c.CallsSucceeded)
	fmt.Printf("  CallsFailed: %+v\n", c.CallsFailed)
	fmt.Printf("  LastCallStartedTimestamp: %+v\n", c.LastCallStartedTimestamp)
	fmt.Printf("  NestedChans: %+v\n", c.NestedChans)
	fmt.Printf("  SubChans: %+v\n", c.SubChans)
	fmt.Printf("  Sockets: %+v\n", c.Sockets)
	fmt.Println("}")
}

func TestCZChannelMetrics(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	num := 3 // number of backends
	te := newTest(t, e)
	te.maxClientSendMsgSize = newInt(8)
	var svrAddrs []resolver.Address
	te.startServers(&testServer{security: e.security}, num)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()
	for _, a := range te.srvAddrs {
		svrAddrs = append(svrAddrs, resolver.Address{Addr: a})
	}
	r.InitialAddrs(svrAddrs)
	te.resolverScheme = r.Scheme()
	cc := te.clientConn()
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(cc)
	if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}

	const smallSize = 1
	const largeSize = 8

	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(smallSize),
		Payload:      largePayload,
	}

	if _, err := tc.UnaryCall(context.Background(), req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	stream, err := tc.FullDuplexCall(context.Background())
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	defer stream.CloseSend()
	// Here, we just wait for all sockets to be up. In the future, if we implement
	// IDLE, we may need to make several rpc calls to create the sockets.
	time.Sleep(100 * time.Millisecond)
	tcs, _ := channelz.GetTopChannels(0)
	if len(tcs) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tcs))
	}
	if len(tcs[0].SubChans) != num {
		t.Fatalf("There should be %d subchannel not %d", num, len(tcs[0].SubChans))
	}
	var cst, csu, cf int64
	for k := range tcs[0].SubChans {
		sc := channelz.GetSubChannel(k)
		if sc == nil {
			t.Fatalf("got <nil> subchannel")
		}
		cst += sc.CallsStarted
		csu += sc.CallsSucceeded
		cf += sc.CallsFailed
	}
	if cst != 3 {
		t.Fatalf("There should be 3 CallsStarted not %d", cst)
	}
	if csu != 1 {
		t.Fatalf("There should be 1 CallsSucceeded not %d", csu)
	}
	if cf != 1 {
		t.Fatalf("There should be 1 CallsFailed not %d", cf)
	}
	if tcs[0].CallsStarted != 3 {
		t.Fatalf("There should be 3 CallsStarted not %d", tcs[0].CallsStarted)
	}
	if tcs[0].CallsSucceeded != 1 {
		t.Fatalf("There should be 1 CallsSucceeded not %d", tcs[0].CallsSucceeded)
	}
	if tcs[0].CallsFailed != 1 {
		t.Fatalf("There should be 1 CallsFailed not %d", tcs[0].CallsFailed)
	}
}

func prettyPrintServerMetric(s *channelz.ServerMetric) {
	if s == nil {
		return
	}
	fmt.Println("{")
	fmt.Printf("  ID: %+v\n", s.ID)
	fmt.Printf("  RefName: %+v\n", s.RefName)
	fmt.Printf("  CallsStarted: %+v\n", s.CallsStarted)
	fmt.Printf("  CallsSucceeded: %+v\n", s.CallsSucceeded)
	fmt.Printf("  CallsFailed: %+v\n", s.CallsFailed)
	fmt.Printf("  LastCallStartedTimestamp: %+v\n", s.LastCallStartedTimestamp)
	fmt.Printf("  Sockets: %+v\n", s.ListenSockets)
	fmt.Println("}")
}

func TestCZServerMetrics(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	te := newTest(t, e)
	te.maxServerReceiveMsgSize = newInt(8)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}

	const smallSize = 1
	const largeSize = 8

	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(smallSize),
		Payload:      largePayload,
	}
	if _, err := tc.UnaryCall(context.Background(), req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	stream, err := tc.FullDuplexCall(context.Background())
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	defer stream.CloseSend()

	time.Sleep(10 * time.Millisecond)
	ss, _ := channelz.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should only be one server, not %d", len(ss))
	}
	if ss[0].CallsStarted != 3 {
		t.Fatalf("There should be 3 CallsStarted not %d", ss[0].CallsStarted)
	}
	if ss[0].CallsSucceeded != 1 {
		t.Fatalf("There should be 1 CallsSucceeded not %d", ss[0].CallsSucceeded)
	}
	if ss[0].CallsFailed != 1 {
		t.Fatalf("There should be 1 CallsFailed not %d", ss[0].CallsFailed)
	}
}

func prettyPrintSocketMetric(s *channelz.SocketMetric) {
	if s == nil {
		return
	}
	fmt.Println("{")
	fmt.Printf("  ID: %+v\n", s.ID)
	fmt.Printf("  RefName: %+v\n", s.RefName)
	fmt.Printf("  StreamsStarted: %+v\n", s.StreamsStarted)
	fmt.Printf("  StreamsSucceeded: %+v\n", s.StreamsSucceeded)
	fmt.Printf("  StreamsFailed: %+v\n", s.StreamsFailed)
	fmt.Printf("  MessagesSent: %+v\n", s.MessagesSent)
	fmt.Printf("  MessagesReceived: %+v\n", s.MessagesReceived)
	fmt.Printf("  KeepAlivesSent: %+v\n", s.KeepAlivesSent)
	fmt.Printf("  LastLocalStreamCreatedTimestamp: %+v\n", s.LastLocalStreamCreatedTimestamp)
	fmt.Printf("  LastRemoteStreamCreatedTimestamp: %+v\n", s.LastRemoteStreamCreatedTimestamp)
	fmt.Printf("  LastMessageSentTimestamp: %+v\n", s.LastMessageSentTimestamp)
	fmt.Printf("  LastMessageReceivedTimestamp: %+v\n", s.LastMessageReceivedTimestamp)
	fmt.Printf("  LocalFlowControlWindow: %+v\n", s.LocalFlowControlWindow)
	fmt.Printf("  RemoteFlowControlWindow: %+v\n", s.RemoteFlowControlWindow)
	fmt.Printf("  Local: %+v\n", s.Local)
	fmt.Printf("  Remote: %+v\n", s.Remote)
	fmt.Printf("  RemoteName: %+v\n", s.RemoteName)
	fmt.Println("}")
}

type testServiceClientWrapper struct {
	testpb.TestServiceClient
	mu             sync.RWMutex
	streamsCreated int
}

func (t *testServiceClientWrapper) getCurrentStreamID() uint32 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return uint32(2*t.streamsCreated - 1)
}

func (t *testServiceClientWrapper) EmptyCall(ctx context.Context, in *testpb.Empty, opts ...grpc.CallOption) (*testpb.Empty, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamsCreated++
	return t.TestServiceClient.EmptyCall(ctx, in, opts...)
}

func (t *testServiceClientWrapper) UnaryCall(ctx context.Context, in *testpb.SimpleRequest, opts ...grpc.CallOption) (*testpb.SimpleResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamsCreated++
	return t.TestServiceClient.UnaryCall(ctx, in, opts...)
}

func (t *testServiceClientWrapper) StreamingOutputCall(ctx context.Context, in *testpb.StreamingOutputCallRequest, opts ...grpc.CallOption) (testpb.TestService_StreamingOutputCallClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamsCreated++
	return t.TestServiceClient.StreamingOutputCall(ctx, in, opts...)
}

func (t *testServiceClientWrapper) StreamingInputCall(ctx context.Context, opts ...grpc.CallOption) (testpb.TestService_StreamingInputCallClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamsCreated++
	return t.TestServiceClient.StreamingInputCall(ctx, opts...)
}

func (t *testServiceClientWrapper) FullDuplexCall(ctx context.Context, opts ...grpc.CallOption) (testpb.TestService_FullDuplexCallClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamsCreated++
	return t.TestServiceClient.FullDuplexCall(ctx, opts...)
}

func (t *testServiceClientWrapper) HalfDuplexCall(ctx context.Context, opts ...grpc.CallOption) (testpb.TestService_HalfDuplexCallClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.streamsCreated++
	return t.TestServiceClient.HalfDuplexCall(ctx, opts...)
}

func doSuccessfulUnaryCall(tc testpb.TestServiceClient, t *testing.T) {
	if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
}

func doServerSideFailedUnaryCall(tc testpb.TestServiceClient, t *testing.T) {
	const smallSize = 1
	const largeSize = 2000

	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(smallSize),
		Payload:      largePayload,
	}
	if _, err := tc.UnaryCall(context.Background(), req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}
}

// This func is to be used to test server side counting of streams succeeded.
// It cannot be used for client side counting due to race between receiving
// server trailer (streamsSucceeded++) and CloseStream on error (streamsFailed++)
// on client side.
func doClientSideFailedUnaryCall(tc testpb.TestServiceClient, t *testing.T) {
	const smallSize = 1
	const largeSize = 2000

	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(largeSize),
		Payload:      smallPayload,
	}
	if _, err := tc.UnaryCall(context.Background(), req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}
}

func doClientSideInitiatedFailedStream(tc testpb.TestServiceClient, t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want <nil>", err)
	}

	const smallSize = 1
	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}

	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: []*testpb.ResponseParameters{
			{Size: smallSize},
		},
		Payload: smallPayload,
	}

	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = %v, want <nil>", stream, err)
	}
	// By canceling the call, the client will send rst_stream to end the call, and
	// the stream will failed as a result.
	cancel()
}

// This func is to be used to test client side counting of failed streams.
func doServerSideInitiatedFailedStreamWithRSTStream(tc testpb.TestServiceClient, t *testing.T, l *listenerWrapper) {
	stream, err := tc.FullDuplexCall(context.Background())
	if err != nil {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want <nil>", err)
	}

	const smallSize = 1
	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}

	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: []*testpb.ResponseParameters{
			{Size: smallSize},
		},
		Payload: smallPayload,
	}

	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = %v, want <nil>", stream, err)
	}

	rcw := l.getLastConn()

	if rcw != nil {
		rcw.writeRSTStream(tc.(*testServiceClientWrapper).getCurrentStreamID(), http2.ErrCodeCancel)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatalf("%v.Recv() = %v, want <non-nil>", stream, err)
	}
}

// this func is to be used to test client side counting of failed streams.
func doServerSideInitiatedFailedStreamWithGoAway(tc testpb.TestServiceClient, t *testing.T, l *listenerWrapper) {
	// This call is just to keep the transport from shutting down (socket will be deleted
	// in this case, and we will not be able to get metrics).
	_, err := tc.FullDuplexCall(context.Background())
	if err != nil {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want <nil>", err)
	}

	s, err := tc.FullDuplexCall(context.Background())
	if err != nil {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want <nil>", err)
	}

	rcw := l.getLastConn()
	if rcw != nil {
		rcw.WriteGoAway(tc.(*testServiceClientWrapper).getCurrentStreamID()-2, http2.ErrCodeCancel, []byte{})
	}
	if _, err := s.Recv(); err == nil {
		t.Fatalf("%v.Recv() = %v, want <non-nil>", s, err)
	}
}

// this func is to be used to test client side counting of failed streams.
func doServerSideInitiatedFailedStreamWithClientBreakFlowControl(tc testpb.TestServiceClient, t *testing.T, dw *dialerWrapper) {
	stream, err := tc.FullDuplexCall(context.Background())
	if err != nil {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want <nil>", err)
	}
	// sleep here to make sure header frame being sent before the the data frame we write directly below.
	time.Sleep(10 * time.Millisecond)
	payload := make([]byte, 65537, 65537)
	dw.getRawConnWrapper().WriteRawFrame(http2.FrameData, 0, tc.(*testServiceClientWrapper).getCurrentStreamID(), payload)
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = %v, want error code: %v", stream, err, codes.ResourceExhausted)
	}

}

func doIdleCallToInvokeKeepAlive(tc testpb.TestServiceClient, t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want <nil>", err)
	}
	// 2500ms allow for 2 keepalives (1000ms per round trip)
	time.Sleep(2500 * time.Millisecond)
	cancel()
}

func TestCZClientSocketMetricsStreamsAndMessagesCount(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	te := newTest(t, e)
	te.maxServerReceiveMsgSize = newInt(20)
	te.maxClientReceiveMsgSize = newInt(20)
	rcw, replace := te.startServerWithConnControl(&testServer{security: e.security})
	defer replace()
	defer te.tearDown()
	cc := te.clientConn()
	tc := &testServiceClientWrapper{TestServiceClient: testpb.NewTestServiceClient(cc)}

	doSuccessfulUnaryCall(tc, t)
	time.Sleep(10 * time.Millisecond)
	tchan, _ := channelz.GetTopChannels(0)
	if len(tchan) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tchan))
	}
	if len(tchan[0].SubChans) != 1 {
		t.Fatalf("There should only be one subchannel under top channel %d, not %d", tchan[0].ID, len(tchan[0].SubChans))
	}
	var id int64
	for id = range tchan[0].SubChans {
		break
	}
	sc := channelz.GetSubChannel(id)
	if sc == nil {
		t.Fatalf("There should only be one socket under subchannel %d, not 0", id)
	}
	if len(sc.Sockets) != 1 {
		t.Fatalf("There should only be one socket under subchannel %d, not %d", sc.ID, len(sc.Sockets))
	}
	for id = range sc.Sockets {
		break
	}
	skt := channelz.GetSocket(id)
	if skt.StreamsStarted != 1 || skt.StreamsSucceeded != 1 || skt.MessagesSent != 1 || skt.MessagesReceived != 1 {
		t.Fatalf("channelz.GetSocket(%d), want (StreamsStarted, StreamsSucceeded, MessagesSent, MessagesReceived) = (1, 1, 1, 1), got (%d, %d, %d, %d)", skt.ID, skt.StreamsStarted, skt.StreamsSucceeded, skt.MessagesSent, skt.MessagesReceived)
	}

	doServerSideFailedUnaryCall(tc, t)
	time.Sleep(10 * time.Millisecond)
	skt = channelz.GetSocket(id)
	if skt.StreamsStarted != 2 || skt.StreamsSucceeded != 2 || skt.MessagesSent != 2 || skt.MessagesReceived != 1 {
		t.Fatalf("channelz.GetSocket(%d), want (StreamsStarted, StreamsSucceeded, MessagesSent, MessagesReceived) = (2, 2, 2, 1), got (%d, %d, %d, %d)", skt.ID, skt.StreamsStarted, skt.StreamsSucceeded, skt.MessagesSent, skt.MessagesReceived)
	}

	doClientSideInitiatedFailedStream(tc, t)
	time.Sleep(10 * time.Millisecond)
	skt = channelz.GetSocket(id)
	if skt.StreamsStarted != 3 || skt.StreamsSucceeded != 2 || skt.StreamsFailed != 1 || skt.MessagesSent != 3 || skt.MessagesReceived != 2 {
		t.Fatalf("channelz.GetSocket(%d), want (StreamsStarted, StreamsSucceeded, StreamsFailed, MessagesSent, MessagesReceived) = (3, 2, 1, 3, 2), got (%d, %d, %d, %d, %d)", skt.ID, skt.StreamsStarted, skt.StreamsSucceeded, skt.StreamsFailed, skt.MessagesSent, skt.MessagesReceived)
	}

	doServerSideInitiatedFailedStreamWithRSTStream(tc, t, rcw)
	time.Sleep(10 * time.Millisecond)
	skt = channelz.GetSocket(id)
	if skt.StreamsStarted != 4 || skt.StreamsSucceeded != 2 || skt.StreamsFailed != 2 || skt.MessagesSent != 4 || skt.MessagesReceived != 3 {
		t.Fatalf("channelz.GetSocket(%d), want (StreamsStarted, StreamsSucceeded, StreamsFailed, MessagesSent, MessagesReceived) = (4, 2, 2, 4, 3), got (%d, %d, %d, %d, %d)", skt.ID, skt.StreamsStarted, skt.StreamsSucceeded, skt.StreamsFailed, skt.MessagesSent, skt.MessagesReceived)
	}

	doServerSideInitiatedFailedStreamWithGoAway(tc, t, rcw)
	time.Sleep(10 * time.Millisecond)
	skt = channelz.GetSocket(id)
	if skt.StreamsStarted != 6 || skt.StreamsSucceeded != 2 || skt.StreamsFailed != 3 || skt.MessagesSent != 4 || skt.MessagesReceived != 3 {
		t.Fatalf("channelz.GetSocket(%d), want (StreamsStarted, StreamsSucceeded, StreamsFailed, MessagesSent, MessagesReceived) = (6, 2, 3, 4, 3), got (%d, %d, %d, %d, %d)", skt.ID, skt.StreamsStarted, skt.StreamsSucceeded, skt.StreamsFailed, skt.MessagesSent, skt.MessagesReceived)
	}
}

// This test is to complete TestCZClientSocketMetricsStreamsAndMessagesCount and
// TestCZServerSocketMetricsStreamsAndMessagesCount by adding the test case of
// server sending RST_STREAM to client due to client side flow control violation.
// It is separated from other cases due to setup incompatibly, i.e. max receive
// size violation will mask flow control violation.
func TestCZClientAndServerSocketMetricsStreamsCountFlowControlRSTStream(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	te := newTest(t, e)
	te.serverInitialWindowSize = 65536
	// Avoid overflowing connection level flow control window, which will lead to
	// transport being closed.
	te.serverInitialConnWindowSize = 65536 * 2
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	cc, dw := te.clientConnWithConnControl()
	tc := &testServiceClientWrapper{TestServiceClient: testpb.NewTestServiceClient(cc)}

	doServerSideInitiatedFailedStreamWithClientBreakFlowControl(tc, t, dw)
	time.Sleep(10 * time.Millisecond)
	tchan, _ := channelz.GetTopChannels(0)
	if len(tchan) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tchan))
	}
	if len(tchan[0].SubChans) != 1 {
		t.Fatalf("There should only be one subchannel under top channel %d, not %d", tchan[0].ID, len(tchan[0].SubChans))
	}
	var id int64
	for id = range tchan[0].SubChans {
		break
	}
	sc := channelz.GetSubChannel(id)
	if sc == nil {
		t.Fatalf("There should only be one socket under subchannel %d, not 0", id)
	}
	if len(sc.Sockets) != 1 {
		t.Fatalf("There should only be one socket under subchannel %d, not %d", sc.ID, len(sc.Sockets))
	}
	for id = range sc.Sockets {
		break
	}
	skt := channelz.GetSocket(id)
	if skt.StreamsStarted != 1 || skt.StreamsSucceeded != 0 || skt.StreamsFailed != 1 {
		t.Fatalf("channelz.GetSocket(%d), want (StreamsStarted, StreamsSucceeded, StreamsFailed) = (1, 0, 1), got (%d, %d, %d)", skt.ID, skt.StreamsStarted, skt.StreamsSucceeded, skt.StreamsFailed)
	}
	ss, _ := channelz.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should only be one server, not %d", len(ss))
	}

	ns, _ := channelz.GetServerSockets(ss[0].ID, 0)
	if len(ns) != 1 {
		t.Fatalf("There should be one server normal socket, not %d", len(ns))
	}
	if ns[0].StreamsStarted != 1 || ns[0].StreamsSucceeded != 0 || ns[0].StreamsFailed != 1 {
		t.Fatalf("Server socket metric with ID %d, want (StreamsStarted, StreamsSucceeded, StreamsFailed) = (1, 0, 1), got (%d, %d, %d)", ns[0].ID, skt.StreamsStarted, skt.StreamsSucceeded, skt.StreamsFailed)
	}
}

func TestCZClientAndServerSocketMetricsFlowControl(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	te := newTest(t, e)
	// disable BDP
	te.serverInitialWindowSize = 65536
	te.serverInitialConnWindowSize = 65536
	te.clientInitialWindowSize = 65536
	te.clientInitialConnWindowSize = 65536
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	for i := 0; i < 10; i++ {
		doSuccessfulUnaryCall(tc, t)
	}

	time.Sleep(10 * time.Millisecond)
	tchan, _ := channelz.GetTopChannels(0)
	if len(tchan) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tchan))
	}
	if len(tchan[0].SubChans) != 1 {
		t.Fatalf("There should only be one subchannel under top channel %d, not %d", tchan[0].ID, len(tchan[0].SubChans))
	}
	var id int64
	for id = range tchan[0].SubChans {
		break
	}
	sc := channelz.GetSubChannel(id)
	if sc == nil {
		t.Fatalf("There should only be one socket under subchannel %d, not 0", id)
	}
	if len(sc.Sockets) != 1 {
		t.Fatalf("There should only be one socket under subchannel %d, not %d", sc.ID, len(sc.Sockets))
	}
	for id = range sc.Sockets {
		break
	}
	skt := channelz.GetSocket(id)
	// 65536 - 5 (Length-Prefixed-Message size) * 10 = 65486
	if skt.LocalFlowControlWindow != 65536 || skt.RemoteFlowControlWindow != 65486 {
		t.Fatalf("(LocalFlowControlWindow, RemoteFlowControlWindow) size should be (65536, 65486), not (%d, %d)", skt.LocalFlowControlWindow, skt.RemoteFlowControlWindow)
	}
	ss, _ := channelz.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should only be one server, not %d", len(ss))
	}
	ns, _ := channelz.GetServerSockets(ss[0].ID, 0)
	if ns[0].LocalFlowControlWindow != 65536 || ns[0].RemoteFlowControlWindow != 65486 {
		t.Fatalf("(LocalFlowControlWindow, RemoteFlowControlWindow) size should be (65536, 65486), not (%d, %d)", ns[0].LocalFlowControlWindow, ns[0].RemoteFlowControlWindow)
	}
}

func TestCZClientSocketMetricsKeepAlive(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	te := newTest(t, e)
	te.cliKeepAlive = &keepalive.ClientParameters{Time: 500 * time.Millisecond, Timeout: 500 * time.Millisecond}
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	doIdleCallToInvokeKeepAlive(tc, t)

	time.Sleep(10 * time.Millisecond)
	tchan, _ := channelz.GetTopChannels(0)
	if len(tchan) != 1 {
		t.Fatalf("There should only be one top channel, not %d", len(tchan))
	}
	if len(tchan[0].SubChans) != 1 {
		t.Fatalf("There should only be one subchannel under top channel %d, not %d", tchan[0].ID, len(tchan[0].SubChans))
	}
	var id int64
	for id = range tchan[0].SubChans {
		break
	}
	sc := channelz.GetSubChannel(id)
	if sc == nil {
		t.Fatalf("There should only be one socket under subchannel %d, not 0", id)
	}
	if len(sc.Sockets) != 1 {
		t.Fatalf("There should only be one socket under subchannel %d, not %d", sc.ID, len(sc.Sockets))
	}
	for id = range sc.Sockets {
		break
	}
	time.Sleep(10 * time.Millisecond)
	skt := channelz.GetSocket(id)
	if skt.KeepAlivesSent != 2 { // doIdleCallToInvokeKeepAlive func is set up to send 2 KeepAlives.
		t.Fatalf("There should be 2 KeepAlives sent, not %d", skt.KeepAlivesSent)
	}
}

func TestCZServerSocketMetricsStreamsAndMessagesCount(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	te := newTest(t, e)
	te.maxServerReceiveMsgSize = newInt(20)
	te.maxClientReceiveMsgSize = newInt(20)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	cc, _ := te.clientConnWithConnControl()
	tc := &testServiceClientWrapper{TestServiceClient: testpb.NewTestServiceClient(cc)}
	ss, _ := channelz.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should only be one server, not %d", len(ss))
	}

	doSuccessfulUnaryCall(tc, t)
	time.Sleep(10 * time.Millisecond)
	ns, _ := channelz.GetServerSockets(ss[0].ID, 0)
	if ns[0].StreamsStarted != 1 || ns[0].StreamsSucceeded != 1 || ns[0].MessagesSent != 1 || ns[0].MessagesReceived != 1 {
		t.Fatalf("Server socket metric with ID %d, want (StreamsStarted, StreamsSucceeded, MessagesSent, MessagesReceived) = (1, 1, 1, 1), got (%d, %d, %d, %d)", ns[0].ID, ns[0].StreamsStarted, ns[0].StreamsSucceeded, ns[0].MessagesSent, ns[0].MessagesReceived)
	}

	doServerSideFailedUnaryCall(tc, t)
	time.Sleep(10 * time.Millisecond)
	ns, _ = channelz.GetServerSockets(ss[0].ID, 0)
	if ns[0].StreamsStarted != 2 || ns[0].StreamsSucceeded != 2 || ns[0].MessagesSent != 1 || ns[0].MessagesReceived != 1 {
		t.Fatalf("Server socket metric with ID %d, want (StreamsStarted, StreamsSucceeded, MessagesSent, MessagesReceived) = (2, 2, 1, 1), got (%d, %d, %d, %d)", ns[0].ID, ns[0].StreamsStarted, ns[0].StreamsSucceeded, ns[0].MessagesSent, ns[0].MessagesReceived)
	}

	doClientSideFailedUnaryCall(tc, t)
	time.Sleep(10 * time.Millisecond)
	ns, _ = channelz.GetServerSockets(ss[0].ID, 0)
	if ns[0].StreamsStarted != 3 || ns[0].StreamsSucceeded != 3 || ns[0].MessagesSent != 2 || ns[0].MessagesReceived != 2 {
		t.Fatalf("Server socket metric with ID %d, want (StreamsStarted, StreamsSucceeded, MessagesSent, MessagesReceived) = (3, 3, 2, 2), got (%d, %d, %d, %d)", ns[0].ID, ns[0].StreamsStarted, ns[0].StreamsSucceeded, ns[0].MessagesSent, ns[0].MessagesReceived)
	}

	doClientSideInitiatedFailedStream(tc, t)
	time.Sleep(10 * time.Millisecond)
	ns, _ = channelz.GetServerSockets(ss[0].ID, 0)
	if ns[0].StreamsStarted != 4 || ns[0].StreamsSucceeded != 3 || ns[0].StreamsFailed != 1 || ns[0].MessagesSent != 3 || ns[0].MessagesReceived != 3 {
		t.Fatalf("Server socket metric with ID %d, want (StreamsStarted, StreamsSucceeded, StreamsFailed, MessagesSent, MessagesReceived) = (4, 3,1, 3, 3), got (%d, %d, %d, %d)", ns[0].ID, ns[0].StreamsStarted, ns[0].StreamsSucceeded, ns[0].MessagesSent, ns[0].MessagesReceived)
	}
}

func TestCZServerSocketMetricsKeepAlive(t *testing.T) {
	defer leakcheck.Check(t)
	turnOnChannelzAndClearPreviousChannelzData()
	e := tcpClearRREnv
	te := newTest(t, e)
	te.svrKeepAlive = &keepalive.ServerParameters{Time: 500 * time.Millisecond, Timeout: 500 * time.Millisecond}
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	doIdleCallToInvokeKeepAlive(tc, t)

	time.Sleep(10 * time.Millisecond)
	ss, _ := channelz.GetServers(0)
	if len(ss) != 1 {
		t.Fatalf("There should be one server, not %d", len(ss))
	}
	ns, _ := channelz.GetServerSockets(ss[0].ID, 0)
	if len(ns) != 1 {
		t.Fatalf("There should be one server normal socket, not %d", len(ns))
	}
	if ns[0].KeepAlivesSent != 2 { // doIdleCallToInvokeKeepAlive func is set up to send 2 KeepAlives.
		t.Fatalf("There should be 2 KeepAlives sent, not %d", ns[0].KeepAlivesSent)
	}
}
