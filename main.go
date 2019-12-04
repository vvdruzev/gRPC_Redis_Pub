package main

import (
	"context"
	"io/ioutil"
	"log"
	"gopkg.in/yaml.v2"
	"fmt"
	"os"
	"flag"
	"google.golang.org/grpc"
	"sync"
	"io"
	"github.com/go-redis/redis"
)

type conf struct {
	MaxTimeout      int      `yml:MaxTimeout`
	MinTimeout      int      `yml:mintimeout`
	URLs            []string `yml:urls`
	NumberOfRequest int      `yml:numberofrequest`
}

func (c *conf) getConf() *conf {
	yamlFile, err := ioutil.ReadFile("config.yml")
	if err != nil {
		log.Printf("yamlFile.Get err   #%v ", err)
	}
	err = yaml.Unmarshal(yamlFile, c)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	return c
}

const (
	listenAddr string = "127.0.0.1:8082"
)

func Usage() {
	fmt.Fprint(os.Stderr, "Usage of ", os.Args[0], ":\n")
	flag.PrintDefaults()
	fmt.Fprint(os.Stderr, "\n")
}

func main() {
	flag.Usage = Usage
	server := flag.Bool("server", false, "Run server")
	var c conf
	c.getConf()
	flag.Parse()
	if *server {
		r := redis.NewClient(&redis.Options{
			Addr:     "localhost:6379", // use default Addr
			Password: "",               // no password set
			DB:       0,                // use default DB
		})

		err := StartMicroservice(r, listenAddr, c.MinTimeout, c.MaxTimeout, c.NumberOfRequest, c.URLs)
		if err != nil {
			log.Fatalf("cant start server initial: %v", err)
		}
	} else {
		var conn *grpc.ClientConn
		conn, err := grpc.Dial(
			listenAddr,
			grpc.WithInsecure(),
		)
		if err != nil {
			log.Fatalf("cant connect to grpc: %v", err)
		}
		defer conn.Close()

		adm := NewAdminClient(conn)
		wg := &sync.WaitGroup{}
		logData := []*Data{}
		var mu sync.Mutex
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				dataStream, err := adm.GetRandomDataStream(context.Background(), &Nothing{})
				if err != nil {
					fmt.Println("Datastream Error: ", err)
					return
				}
				for {
					evt, err := dataStream.Recv()
					//log.Println("logger 1", err, evt)
					if err == io.EOF {
						break
					}
					if err != nil {
						log.Printf("unexpected error: %v, awaiting event", err)
						break
					}
					mu.Lock()
					logData = append(logData, evt)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if len(logData) != c.NumberOfRequest*1000 {
			log.Printf("Number of responses wrong, want: %d, got: %d", c.NumberOfRequest*1000, len(logData))
		}else {
			log.Printf("-  %d request were created, %d responses were received", 1000, c.NumberOfRequest*1000)
		}
	}
}

