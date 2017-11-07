package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/examples/metadata/helloworld"
	"google.golang.org/grpc/metadata"
)

// var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

func unaryCall(cli helloworld.GreeterClient, wg *sync.WaitGroup) {
	/* Unary Call*/
	md := metadata.Pairs("a-bin", "lala", "B", "haha")
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	var header, trailer metadata.MD
	for i := 0; i < 100; i++ {
		_, err := cli.SayHello(ctx, &helloworld.HelloRequest{Name: "hahaha"}, grpc.Header(&header), grpc.Trailer(&trailer))
		if err != nil {
			log.Fatalf("Sayhello failed due to %v", err)
		} else {
			// log.Printf("Sayhello succeeded. Resp: %+v", *resp)
			// fmt.Println(header, trailer)
		}
	}
	wg.Done()
}

func cliStream(cli helloworld.GreeterClient, wg *sync.WaitGroup) {
	/* Client Streaming Call*/
	fmt.Println("\n\n")
	md := metadata.Pairs("a-bin", "lala", "B", "haha")

	ctx := metadata.NewOutgoingContext(context.Background(), md)
	s, err := cli.ClientStreamSayHello(ctx)
	if err != nil {
		log.Fatalf("cli stream say hello failed due to %v", err)
	} else {
		for i := 0; i < 10; i++ {
			err = s.Send(&helloworld.HelloRequest{Name: fmt.Sprintf("welcome %d", i)})
			if err != nil {
				log.Fatalf("cli stream say hello send failed due to %v", err)
			}
			time.Sleep(500 * time.Millisecond)
		}
		_, err := s.CloseAndRecv()
		if err != nil {
			log.Fatalf("error receiving response due to %v", err)
		}
		// fmt.Printf("resp: %+v\n", resp)
	}
	md, err = s.Header()
	if err != nil {
		log.Fatalf("error receiving header due to %v", err)
	}
	// fmt.Printf("header md %+v\n", md)
	md = s.Trailer()
	// fmt.Printf("trailer md %+v\n", md)
	wg.Done()

}

func svrStream(cli helloworld.GreeterClient, wg *sync.WaitGroup) {
	/* Server Streaming Call*/
	fmt.Println("\n\n")
	md := metadata.Pairs("a-bin", "lala", "B", "haha")
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	ss, err := cli.ServerStreamSayHello(ctx, &helloworld.HelloRequest{Name: "who am I"})
	if err != nil {
		log.Fatalf("error receiving response due to %v", err)
	}
	for {
		_, err := ss.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("error receiving response due tp %v", err)
		}
		// fmt.Printf("resp: %+v\n", resp)
	}
	wg.Done()

}

func submain(wgg *sync.WaitGroup) {
	// flag.Parse()
	// if *cpuprofile != "" {
	// 	f, err := os.Create(*cpuprofile)
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	pprof.StartCPUProfile(f)
	// 	defer pprof.StopCPUProfile()
	// }
	cc, err := grpc.Dial(":10001", grpc.WithInsecure(), grpc.WithBalancerBuilder(balancer.Get("roundrobin")))
	defer cc.Close()

	if err != nil {
		log.Fatalf("failed to dial %v", err)
	}
	cli := helloworld.NewGreeterClient(cc)

	var wg sync.WaitGroup
	wg.Add(3)
	go unaryCall(cli, &wg)
	go cliStream(cli, &wg)
	go svrStream(cli, &wg)
	wg.Wait()
	time.Sleep(2 * time.Second)
	wgg.Done()
}

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go submain(&wg)
	// go submain(&wg)
	// go submain(&wg)
	wg.Wait()
	time.Sleep(5 * time.Second)
}
