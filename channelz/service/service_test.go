// +build amd64,linux

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

package service_test

import (
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"golang.org/x/net/context"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/channelz"
	"google.golang.org/grpc/channelz/service"
	pb "google.golang.org/grpc/channelz/service_proto"
	"google.golang.org/grpc/connectivity"
)

func init() {
	proto.RegisterType((*OtherSecurityValue)(nil), "grpc.channelz.OtherSecurityValue")
	channelz.TurnOn()
}

// emptyTime is used for detecting unset value of time.Time type.
// For go1.7 and earlier, ptypes.Timestamp will fill in the loc field of time.Time
// with &utcLoc. However zero value of a time.Time type value loc field is nil.
// This behavior will make reflect.DeepEqual fail upon unset time.Time field,
// and cause false positive fatal error.
var emptyTime time.Time

type dummyChannel struct {
	state                    connectivity.State
	target                   string
	callsStarted             int64
	callsSucceeded           int64
	callsFailed              int64
	lastCallStartedTimestamp time.Time
}

func (d *dummyChannel) ChannelzMetric() *channelz.ChannelInternalMetric {
	return &channelz.ChannelInternalMetric{
		State:                    d.state,
		Target:                   d.target,
		CallsStarted:             d.callsStarted,
		CallsSucceeded:           d.callsSucceeded,
		CallsFailed:              d.callsFailed,
		LastCallStartedTimestamp: d.lastCallStartedTimestamp,
	}
}

type dummyServer struct {
	callsStarted             int64
	callsSucceeded           int64
	callsFailed              int64
	lastCallStartedTimestamp time.Time
}

func (d *dummyServer) ChannelzMetric() *channelz.ServerInternalMetric {
	return &channelz.ServerInternalMetric{
		CallsStarted:             d.callsStarted,
		CallsSucceeded:           d.callsSucceeded,
		CallsFailed:              d.callsFailed,
		LastCallStartedTimestamp: d.lastCallStartedTimestamp,
	}
}

type dummySocket struct {
	streamsStarted                   int64
	streamsSucceeded                 int64
	streamsFailed                    int64
	messagesSent                     int64
	messagesReceived                 int64
	keepAlivesSent                   int64
	lastLocalStreamCreatedTimestamp  time.Time
	lastRemoteStreamCreatedTimestamp time.Time
	lastMessageSentTimestamp         time.Time
	lastMessageReceivedTimestamp     time.Time
	localFlowControlWindow           int64
	remoteFlowControlWindow          int64
	SocketOptions                    *channelz.SocketOptionData
	localAddr                        net.Addr
	remoteAddr                       net.Addr
	Security                         channelz.SecurityValue
	remoteName                       string
}

func (d *dummySocket) ChannelzMetric() *channelz.SocketInternalMetric {
	return &channelz.SocketInternalMetric{
		StreamsStarted:                   d.streamsStarted,
		StreamsSucceeded:                 d.streamsSucceeded,
		StreamsFailed:                    d.streamsFailed,
		MessagesSent:                     d.messagesSent,
		MessagesReceived:                 d.messagesReceived,
		KeepAlivesSent:                   d.keepAlivesSent,
		LastLocalStreamCreatedTimestamp:  d.lastLocalStreamCreatedTimestamp,
		LastRemoteStreamCreatedTimestamp: d.lastRemoteStreamCreatedTimestamp,
		LastMessageSentTimestamp:         d.lastMessageSentTimestamp,
		LastMessageReceivedTimestamp:     d.lastMessageReceivedTimestamp,
		LocalFlowControlWindow:           d.localFlowControlWindow,
		RemoteFlowControlWindow:          d.remoteFlowControlWindow,
		SocketOptions:                    d.SocketOptions,
		LocalAddr:                        d.localAddr,
		RemoteAddr:                       d.remoteAddr,
		Security:                         d.Security,
		RemoteName:                       d.remoteName,
	}
}

func channelProtoToStruct(c *pb.Channel) *dummyChannel {
	dc := &dummyChannel{}
	pdata := c.GetData()
	switch pdata.GetState().GetState() {
	case pb.ChannelConnectivityState_UNKNOWN:
		// TODO: what should we set here?
	case pb.ChannelConnectivityState_IDLE:
		dc.state = connectivity.Idle
	case pb.ChannelConnectivityState_CONNECTING:
		dc.state = connectivity.Connecting
	case pb.ChannelConnectivityState_READY:
		dc.state = connectivity.Ready
	case pb.ChannelConnectivityState_TRANSIENT_FAILURE:
		dc.state = connectivity.TransientFailure
	case pb.ChannelConnectivityState_SHUTDOWN:
		dc.state = connectivity.Shutdown
	}
	dc.target = pdata.GetTarget()
	dc.callsStarted = pdata.CallsStarted
	dc.callsSucceeded = pdata.CallsSucceeded
	dc.callsFailed = pdata.CallsFailed
	if t, err := ptypes.Timestamp(pdata.GetLastCallStartedTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			dc.lastCallStartedTimestamp = t
		}
	}
	return dc
}

func serverProtoToStruct(s *pb.Server) *dummyServer {
	ds := &dummyServer{}
	pdata := s.GetData()
	ds.callsStarted = pdata.CallsStarted
	ds.callsSucceeded = pdata.CallsSucceeded
	ds.callsFailed = pdata.CallsFailed
	if t, err := ptypes.Timestamp(pdata.GetLastCallStartedTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastCallStartedTimestamp = t
		}
	}
	return ds
}

func protoToLinger(protoLinger *pb.SocketOptionLinger) *unix.Linger {
	linger := &unix.Linger{}
	if protoLinger.GetActive() {
		linger.Onoff = 1
	}
	if protoDuration := protoLinger.GetDuration(); protoDuration != nil {
		if dur, err := ptypes.Duration(protoDuration); err == nil {
			linger.Linger = int32(int64(dur) / 1e9)
		}
	}
	return linger
}

func protoToTime(protoTime *pb.SocketOptionTimeout) *unix.Timeval {
	timeout := &unix.Timeval{}
	if protoDuration := protoTime.GetDuration(); protoDuration != nil {
		if dur, err := ptypes.Duration(protoDuration); err == nil {
			timeout.Sec = int64(dur) / 1e9
			timeout.Usec = (int64(dur) - timeout.Sec*1e9) / 1e3
		}
	}
	return timeout
}

func protoToSecurity(protoSecurity *pb.Security) channelz.SecurityValue {
	switch v := protoSecurity.Model.(type) {
	case *pb.Security_Tls_:
		return &channelz.TLSSecurityValue{v.Tls.GetStandardName(), v.Tls.GetLocalCertificate(), v.Tls.GetRemoteCertificate()}
	case *pb.Security_Other:
		sv := &channelz.OtherSecurityValue{Name: v.Other.GetName()}
		var x ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(v.Other.GetValue(), &x); err == nil {
			sv.Value = x.Message
		}
		return sv
	}
	return nil
}

func protoToSocketOption(skopts []*pb.SocketOption) *channelz.SocketOptionData {
	skdata := &channelz.SocketOptionData{}
	for _, opt := range skopts {
		switch opt.GetName() {
		case "SO_LINGER":
			protoLinger := &pb.SocketOptionLinger{}
			err := ptypes.UnmarshalAny(opt.GetAdditional(), protoLinger)
			if err == nil {
				skdata.Linger = protoToLinger(protoLinger)
			}
		case "SO_RCVTIMEO":
			protoTimeout := &pb.SocketOptionTimeout{}
			err := ptypes.UnmarshalAny(opt.GetAdditional(), protoTimeout)
			if err == nil {
				skdata.RecvTimeout = protoToTime(protoTimeout)
			}
		case "SO_SNDTIMEO":
			protoTimeout := &pb.SocketOptionTimeout{}
			err := ptypes.UnmarshalAny(opt.GetAdditional(), protoTimeout)
			if err == nil {
				skdata.SendTimeout = protoToTime(protoTimeout)
			}
		case "TCP_INFO":
			tcpi := &pb.SocketOptionTcpInfo{}
			err := ptypes.UnmarshalAny(opt.GetAdditional(), tcpi)
			if err == nil {
				skdata.TCPInfo = &unix.TCPInfo{
					State:          uint8(tcpi.TcpiState),
					Ca_state:       uint8(tcpi.TcpiCaState),
					Retransmits:    uint8(tcpi.TcpiRetransmits),
					Probes:         uint8(tcpi.TcpiProbes),
					Backoff:        uint8(tcpi.TcpiBackoff),
					Options:        uint8(tcpi.TcpiOptions),
					Rto:            tcpi.TcpiRto,
					Ato:            tcpi.TcpiAto,
					Snd_mss:        tcpi.TcpiSndMss,
					Rcv_mss:        tcpi.TcpiRcvMss,
					Unacked:        tcpi.TcpiUnacked,
					Sacked:         tcpi.TcpiSacked,
					Lost:           tcpi.TcpiLost,
					Retrans:        tcpi.TcpiRetrans,
					Fackets:        tcpi.TcpiFackets,
					Last_data_sent: tcpi.TcpiLastDataSent,
					Last_ack_sent:  tcpi.TcpiLastAckSent,
					Last_data_recv: tcpi.TcpiLastDataRecv,
					Last_ack_recv:  tcpi.TcpiLastAckRecv,
					Pmtu:           tcpi.TcpiPmtu,
					Rcv_ssthresh:   tcpi.TcpiRcvSsthresh,
					Rtt:            tcpi.TcpiRtt,
					Rttvar:         tcpi.TcpiRttvar,
					Snd_ssthresh:   tcpi.TcpiSndSsthresh,
					Snd_cwnd:       tcpi.TcpiSndCwnd,
					Advmss:         tcpi.TcpiAdvmss,
					Reordering:     tcpi.TcpiReordering}
			}
		}
	}
	return skdata
}

func protoToAddr(a *pb.Address) net.Addr {
	switch v := a.Address.(type) {
	case *pb.Address_TcpipAddress:
		if port := v.TcpipAddress.GetPort(); port != 0 {
			return &net.TCPAddr{IP: v.TcpipAddress.GetIpAddress(), Port: int(port)}
		}
		return &net.IPAddr{IP: v.TcpipAddress.GetIpAddress()}
	case *pb.Address_UdsAddress_:
		return &net.UnixAddr{Name: v.UdsAddress.GetFilename(), Net: "unix"}
	case *pb.Address_OtherAddress_:
		// TODO:
	}
	return nil
}

func socketProtoToStruct(s *pb.Socket) *dummySocket {
	ds := &dummySocket{}
	pdata := s.GetData()
	ds.streamsStarted = pdata.GetStreamsStarted()
	ds.streamsSucceeded = pdata.GetStreamsSucceeded()
	ds.streamsFailed = pdata.GetStreamsFailed()
	ds.messagesSent = pdata.GetMessagesSent()
	ds.messagesReceived = pdata.GetMessagesReceived()
	ds.keepAlivesSent = pdata.GetKeepAlivesSent()
	if t, err := ptypes.Timestamp(pdata.GetLastLocalStreamCreatedTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastLocalStreamCreatedTimestamp = t
		}
	}
	if t, err := ptypes.Timestamp(pdata.GetLastRemoteStreamCreatedTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastRemoteStreamCreatedTimestamp = t
		}
	}
	if t, err := ptypes.Timestamp(pdata.GetLastMessageSentTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastMessageSentTimestamp = t
		}
	}
	if t, err := ptypes.Timestamp(pdata.GetLastMessageReceivedTimestamp()); err == nil {
		if !t.Equal(emptyTime) {
			ds.lastMessageReceivedTimestamp = t
		}
	}
	if v := pdata.GetLocalFlowControlWindow(); v != nil {
		ds.localFlowControlWindow = v.Value
	}
	if v := pdata.GetRemoteFlowControlWindow(); v != nil {
		ds.remoteFlowControlWindow = v.Value
	}
	if v := pdata.GetOption(); v != nil {
		ds.SocketOptions = protoToSocketOption(v)
	}
	if v := s.GetSecurity(); v != nil {
		ds.Security = protoToSecurity(v)
	}
	if local := s.GetLocal(); local != nil {
		ds.localAddr = protoToAddr(local)
	}
	if remote := s.GetRemote(); remote != nil {
		ds.remoteAddr = protoToAddr(remote)
	}
	ds.remoteName = s.GetRemoteName()
	return ds
}

func convertSocketRefSliceToMap(sktRefs []*pb.SocketRef) map[int64]string {
	m := make(map[int64]string)
	for _, sr := range sktRefs {
		m[sr.SocketId] = sr.Name
	}
	return m
}

type OtherSecurityValue struct {
	LocalCertificate  []byte `protobuf:"bytes,1,opt,name=local_certificate,json=localCertificate,proto3" json:"local_certificate,omitempty"`
	RemoteCertificate []byte `protobuf:"bytes,2,opt,name=remote_certificate,json=remoteCertificate,proto3" json:"remote_certificate,omitempty"`
}

func (m *OtherSecurityValue) Reset()         { *m = OtherSecurityValue{} }
func (m *OtherSecurityValue) String() string { return proto.CompactTextString(m) }
func (*OtherSecurityValue) ProtoMessage()    {}

func TestGetTopChannels(t *testing.T) {
	tcs := []*dummyChannel{
		{
			state:                    connectivity.Connecting,
			target:                   "test.channelz:1234",
			callsStarted:             6,
			callsSucceeded:           2,
			callsFailed:              3,
			lastCallStartedTimestamp: time.Now().UTC(),
		},
		{
			state:                    connectivity.Connecting,
			target:                   "test.channelz:1234",
			callsStarted:             1,
			callsSucceeded:           2,
			callsFailed:              3,
			lastCallStartedTimestamp: time.Now().UTC(),
		},
		{
			state:          connectivity.Shutdown,
			target:         "test.channelz:8888",
			callsStarted:   0,
			callsSucceeded: 0,
			callsFailed:    0,
		},
	}
	channelz.NewChannelzStorage()
	for _, c := range tcs {
		channelz.RegisterChannel(c, 0, "")
	}
	s := service.NewCZServer()
	resp, _ := s.GetTopChannels(context.Background(), &pb.GetTopChannelsRequest{StartChannelId: 0})
	if !resp.GetEnd() {
		t.Fatalf("resp.GetEnd() want true, got %v", resp.GetEnd())
	}
	for i, c := range resp.GetChannel() {
		if !reflect.DeepEqual(channelProtoToStruct(c), tcs[i]) {
			t.Fatalf("dummyChannel: %d, want: %#v, got: %#v", i, tcs[i], channelProtoToStruct(c))
		}
	}
	for i := 0; i < 50; i++ {
		channelz.RegisterChannel(tcs[0], 0, "")
	}
	resp, _ = s.GetTopChannels(context.Background(), &pb.GetTopChannelsRequest{StartChannelId: 0})
	if resp.GetEnd() {
		t.Fatalf("resp.GetEnd() want false, got %v", resp.GetEnd())
	}
}

func TestGetServers(t *testing.T) {
	ss := []*dummyServer{
		{
			callsStarted:             6,
			callsSucceeded:           2,
			callsFailed:              3,
			lastCallStartedTimestamp: time.Now().UTC(),
		},
		{
			callsStarted:             1,
			callsSucceeded:           2,
			callsFailed:              3,
			lastCallStartedTimestamp: time.Now().UTC(),
		},
		{
			callsStarted:             1,
			callsSucceeded:           0,
			callsFailed:              0,
			lastCallStartedTimestamp: time.Now().UTC(),
		},
	}
	channelz.NewChannelzStorage()
	for _, s := range ss {
		channelz.RegisterServer(s, "")
	}
	svr := service.NewCZServer()
	resp, _ := svr.GetServers(context.Background(), &pb.GetServersRequest{StartServerId: 0})
	if !resp.GetEnd() {
		t.Fatalf("resp.GetEnd() want true, got %v", resp.GetEnd())
	}
	for i, s := range resp.GetServer() {
		if !reflect.DeepEqual(serverProtoToStruct(s), ss[i]) {
			t.Fatalf("dummyServer: %d, want: %#v, got: %#v", i, ss[i], serverProtoToStruct(s))
		}
	}
	for i := 0; i < 50; i++ {
		channelz.RegisterServer(ss[0], "")
	}
	resp, _ = svr.GetServers(context.Background(), &pb.GetServersRequest{StartServerId: 0})
	if resp.GetEnd() {
		t.Fatalf("resp.GetEnd() want false, got %v", resp.GetEnd())
	}
}

func TestGetServerSockets(t *testing.T) {
	channelz.NewChannelzStorage()
	svrId := channelz.RegisterServer(&dummyServer{}, "")
	refNames := []string{"listen socket 1", "normal socket 1", "normal socket 2"}
	ids := make([]int64, 3)
	ids[0] = channelz.RegisterListenSocket(&dummySocket{}, svrId, refNames[0])
	ids[1] = channelz.RegisterNormalSocket(&dummySocket{}, svrId, refNames[1])
	ids[2] = channelz.RegisterNormalSocket(&dummySocket{}, svrId, refNames[2])
	svr := service.NewCZServer()
	resp, _ := svr.GetServerSockets(context.Background(), &pb.GetServerSocketsRequest{ServerId: svrId, StartSocketId: 0})
	if !resp.GetEnd() {
		t.Fatalf("resp.GetEnd() want: true, got: %v", resp.GetEnd())
	}
	// GetServerSockets only return normal sockets.
	want := map[int64]string{
		ids[1]: refNames[1],
		ids[2]: refNames[2],
	}
	if !reflect.DeepEqual(convertSocketRefSliceToMap(resp.GetSocketRef()), want) {
		t.Fatalf("GetServerSockets want: %#v, got: %#v", want, resp.GetSocketRef())
	}

	for i := 0; i < 50; i++ {
		channelz.RegisterNormalSocket(&dummySocket{}, svrId, "")
	}
	resp, _ = svr.GetServerSockets(context.Background(), &pb.GetServerSocketsRequest{ServerId: svrId, StartSocketId: 0})
	if resp.GetEnd() {
		t.Fatalf("resp.GetEnd() want false, got %v", resp.GetEnd())
	}
}

func TestGetChannel(t *testing.T) {
	channelz.NewChannelzStorage()
	refNames := []string{"top channel 1", "nested channel 1", "nested channel 2", "nested channel 3"}
	ids := make([]int64, 4)
	ids[0] = channelz.RegisterChannel(&dummyChannel{}, 0, refNames[0])
	ids[1] = channelz.RegisterChannel(&dummyChannel{}, ids[0], refNames[1])
	ids[2] = channelz.RegisterSubChannel(&dummyChannel{}, ids[0], refNames[2])
	ids[3] = channelz.RegisterChannel(&dummyChannel{}, ids[1], refNames[3])
	svr := service.NewCZServer()
	resp, _ := svr.GetChannel(context.Background(), &pb.GetChannelRequest{ChannelId: ids[0]})
	metrics := resp.GetChannel()
	subChans := metrics.GetSubchannelRef()
	if len(subChans) != 1 || subChans[0].GetName() != refNames[2] || subChans[0].GetSubchannelId() != ids[2] {
		t.Fatalf("GetSubChannelRef() want %#v, got %#v", []*pb.SubchannelRef{{ids[2], refNames[2]}}, subChans)
	}
	nestedChans := metrics.GetChannelRef()
	if len(nestedChans) != 1 || nestedChans[0].GetName() != refNames[1] || nestedChans[0].GetChannelId() != ids[1] {
		t.Fatalf("GetChannelRef() want %#v, got %#v", []*pb.ChannelRef{{ids[1], refNames[1]}}, nestedChans)
	}

	resp, _ = svr.GetChannel(context.Background(), &pb.GetChannelRequest{ChannelId: ids[1]})
	metrics = resp.GetChannel()
	nestedChans = metrics.GetChannelRef()
	if len(nestedChans) != 1 || nestedChans[0].GetName() != refNames[3] || nestedChans[0].GetChannelId() != ids[3] {
		t.Fatalf("GetChannelRef() want %#v, got %#v", []*pb.ChannelRef{{ids[3], refNames[3]}}, nestedChans)
	}
}

func TestGetSubChannel(t *testing.T) {
	channelz.NewChannelzStorage()
	refNames := []string{"top channel 1", "sub channel 1", "socket 1", "socket 2"}
	ids := make([]int64, 4)
	ids[0] = channelz.RegisterChannel(&dummyChannel{}, 0, refNames[0])
	ids[1] = channelz.RegisterSubChannel(&dummyChannel{}, ids[0], refNames[1])
	ids[2] = channelz.RegisterNormalSocket(&dummySocket{}, ids[1], refNames[2])
	ids[3] = channelz.RegisterNormalSocket(&dummySocket{}, ids[1], refNames[3])
	svr := service.NewCZServer()
	resp, _ := svr.GetSubchannel(context.Background(), &pb.GetSubchannelRequest{SubchannelId: ids[1]})
	metrics := resp.GetSubchannel()
	want := map[int64]string{
		ids[2]: refNames[2],
		ids[3]: refNames[3],
	}
	if !reflect.DeepEqual(convertSocketRefSliceToMap(metrics.GetSocketRef()), want) {
		t.Fatalf("GetSocketRef() want %#v: got: %#v", want, metrics.GetSocketRef())
	}
}

func TestGetSocket(t *testing.T) {
	channelz.NewChannelzStorage()
	ss := []*dummySocket{
		{
			streamsStarted:                   10,
			streamsSucceeded:                 2,
			streamsFailed:                    3,
			messagesSent:                     20,
			messagesReceived:                 10,
			keepAlivesSent:                   2,
			lastLocalStreamCreatedTimestamp:  time.Now().UTC(),
			lastRemoteStreamCreatedTimestamp: time.Now().UTC(),
			lastMessageSentTimestamp:         time.Now().UTC(),
			lastMessageReceivedTimestamp:     time.Now().UTC(),
			localFlowControlWindow:           65536,
			remoteFlowControlWindow:          1024,
			localAddr:                        &net.TCPAddr{IP: net.ParseIP("1.0.0.1"), Port: 10001},
			remoteAddr:                       &net.TCPAddr{IP: net.ParseIP("12.0.0.1"), Port: 10002},
			remoteName:                       "remote.remote",
		},
		{
			streamsStarted:                   10,
			streamsSucceeded:                 2,
			streamsFailed:                    3,
			messagesSent:                     20,
			messagesReceived:                 10,
			keepAlivesSent:                   2,
			lastRemoteStreamCreatedTimestamp: time.Now().UTC(),
			lastMessageSentTimestamp:         time.Now().UTC(),
			lastMessageReceivedTimestamp:     time.Now().UTC(),
			localFlowControlWindow:           65536,
			remoteFlowControlWindow:          1024,
			localAddr:                        &net.UnixAddr{Name: "file.path", Net: "unix"},
			remoteAddr:                       &net.UnixAddr{Name: "another.path", Net: "unix"},
			remoteName:                       "remote.remote",
		},
		{
			streamsStarted:                  5,
			streamsSucceeded:                2,
			streamsFailed:                   3,
			messagesSent:                    20,
			messagesReceived:                10,
			keepAlivesSent:                  2,
			lastLocalStreamCreatedTimestamp: time.Now().UTC(),
			lastMessageSentTimestamp:        time.Now().UTC(),
			lastMessageReceivedTimestamp:    time.Now().UTC(),
			localFlowControlWindow:          65536,
			remoteFlowControlWindow:         10240,
			localAddr:                       &net.IPAddr{IP: net.ParseIP("1.0.0.1")},
			remoteAddr:                      &net.IPAddr{IP: net.ParseIP("9.0.0.1")},
			remoteName:                      "",
		},
		{
			localAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10001},
		},
		{
			SocketOptions: &channelz.SocketOptionData{
				Linger:      &unix.Linger{Onoff: 1, Linger: 2},
				RecvTimeout: &unix.Timeval{Sec: 10, Usec: 1},
				SendTimeout: &unix.Timeval{},
				TCPInfo:     &unix.TCPInfo{State: 1},
			},
		},
		{
			Security: &channelz.TLSSecurityValue{
				StandardName:      "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				RemoteCertificate: []byte{48, 130, 2, 156, 48, 130, 2, 5, 160},
			},
		},
		{
			Security: &channelz.OtherSecurityValue{
				Name: "XXXX",
			},
		},
		{
			Security: &channelz.OtherSecurityValue{
				Name:  "YYYY",
				Value: &OtherSecurityValue{LocalCertificate: []byte{1, 2, 3}, RemoteCertificate: []byte{4, 5, 6}},
			},
		},
	}
	svr := service.NewCZServer()
	ids := make([]int64, len(ss))
	svrId := channelz.RegisterServer(&dummyServer{}, "")
	for i, s := range ss {
		ids[i] = channelz.RegisterNormalSocket(s, svrId, strconv.Itoa(i))
	}
	for i, s := range ss {
		resp, _ := svr.GetSocket(context.Background(), &pb.GetSocketRequest{SocketId: ids[i]})
		metrics := resp.GetSocket()
		if !reflect.DeepEqual(metrics.GetRef(), &pb.SocketRef{SocketId: ids[i], Name: strconv.Itoa(i)}) || !reflect.DeepEqual(socketProtoToStruct(metrics), s) {
			t.Fatalf("resp.GetSocket() want: metrics.GetRef() = %#v and %#v, got: metrics.GetRef() = %#v and %#v", &pb.SocketRef{SocketId: ids[i], Name: strconv.Itoa(i)}, s, metrics.GetRef(), socketProtoToStruct(metrics))
		}
	}
}
