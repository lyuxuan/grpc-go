package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	context "golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/examples/metadata/helloworld"
	"google.golang.org/grpc/metadata"
)

type helloWorldServer struct{}

func (*helloWorldServer) SayHello(ctx context.Context, req *helloworld.HelloRequest) (*helloworld.HelloReply, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		for k, v := range md {
			fmt.Printf("key %s, val %v\n", k, v)
		}
	}
	nmd := metadata.Join(md, metadata.Pairs("c", "meme"))
	fmt.Println(md["a-bin"])
	grpc.SetHeader(ctx, nmd)
	grpc.SendHeader(ctx, metadata.Pairs("c", "oo"))
	grpc.SetTrailer(ctx, nmd)
	return &helloworld.HelloReply{Message: "hello " + req.GetName()}, nil
}
func (*helloWorldServer) ClientStreamSayHello(s helloworld.Greeter_ClientStreamSayHelloServer) error {
	str := ""
	md, ok := metadata.FromIncomingContext(s.Context())
	if ok {
		fmt.Println("\n\n")
		for k, v := range md {
			fmt.Printf("key %s, val %v\n", k, v)
		}
	}
	s.SendHeader(metadata.Pairs("i_am", "robot"))
	i := 0
	for {
		req, err := s.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("error recving request due to %v", err)
		}
		str += " " + req.GetName()
		i++
	}
	s.SendAndClose(&helloworld.HelloReply{Message: str})
	s.SetTrailer(metadata.Pairs("20", "12"))

	return nil
}

func (*helloWorldServer) ServerStreamSayHello(req *helloworld.HelloRequest, s helloworld.Greeter_ServerStreamSayHelloServer) error {
	name := req.GetName()
	md, ok := metadata.FromIncomingContext(s.Context())
	if ok {
		fmt.Println("\n\n")
		for k, v := range md {
			fmt.Printf("key %s, val %v\n", k, v)
		}
	}
	for i := 0; i < 5; i++ {
		err := s.Send(&helloworld.HelloReply{Message: fmt.Sprintf("%s: you are %d", name, i)})
		if err != nil {
			log.Fatalf("error sending response due to %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}
func (*helloWorldServer) BidiSayHello(helloworld.Greeter_BidiSayHelloServer) error {
	return nil
}

func main() {
	lis, err := net.Listen("tcp", ":10001")
	if err != nil {
		log.Fatalf("failed to listen %v", err)
	}
	svr := grpc.NewServer()
	helloworld.RegisterGreeterServer(svr, &helloWorldServer{})
	svr.Serve(lis)
}
