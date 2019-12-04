package main

import (
	"fmt"
	"google.golang.org/grpc"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
	"strings"
	"testStream/memolock"
	"github.com/go-redis/redis"
)

type Manager struct {
	NumberOfRequest int
	MinTimeout      int
	MaxTimeuot      int
	URLs            []string
	mu              *sync.Mutex
	proxy           string
	queryMemolock   *memolock.RedisMemoLock
}

func NewManager(r *redis.Client, minTimeout int, maxTimeuot int, numberOfRequest int, uRLs []string) *Manager {
	m := &Manager{
		URLs:            uRLs,
		MinTimeout:      minTimeout,
		MaxTimeuot:      maxTimeuot,
		NumberOfRequest: numberOfRequest,
	}
	m.proxy = getProxy()
	m.queryMemolock, _ = memolock.NewRedisMemoLock(r, "query")
	return m
}

type AdminServerManager struct {
	*Manager
}

func NewAdminServer(manager *Manager) *AdminServerManager {
	return &AdminServerManager{manager}
}

func (as *AdminServerManager) AdminStreamInterceptor1(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (error) {
	err := handler(srv, ss)
	return err
}

func (as *AdminServerManager) GetRandomDataStream(nothing *Nothing, stream Admin_GetRandomDataStreamServer) error {
	out := make(chan *Data, as.NumberOfRequest)
	for i := 0; i < as.NumberOfRequest; i++ {
		go makeRequest(out, as.Manager)
	}
	for i := 0; i < as.NumberOfRequest; i++ {
		c := <-out
		if strings.Contains(c.Data, "/www.bbc.co.uk") {
			fmt.Println(c.Data)
		}
		if err := stream.Send(c); err != nil {

			fmt.Println("error send:  ", err)
		}

	}
	return nil
}

func StartMicroservice(r *redis.Client, listenAddr string, minTimeout int, maxTimeuot int, numberOfRequest int, uRLs []string) error {

	manager := NewManager(r, minTimeout, maxTimeuot, numberOfRequest, uRLs)

	adminServer := NewAdminServer(manager)
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalln("cant listet port", err)
	}

	server := grpc.NewServer(grpc.StreamInterceptor(adminServer.AdminStreamInterceptor1))
	RegisterAdminServer(server, adminServer)
	fmt.Printf("listen addres  %s\n ", listenAddr)
	return server.Serve(lis)

}

func makeRequest(out chan *Data, m *Manager) {
	rand.Seed(time.Now().UnixNano())
	c := new(Data)
	url := m.URLs[rand.Intn(len(m.URLs)-1)]
	requestTimeout := time.Duration(rand.Intn(m.MaxTimeuot-m.MinTimeout)+m.MinTimeout) * time.Second

	c.Data, _ = m.queryMemolock.GetResource(url, requestTimeout, func() (string, time.Duration, error) {
		result, _ := getPage(url)

		return string(result), requestTimeout, nil
	})

	out <- c
}
