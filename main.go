package main

import (
	"log"
	"net/http"

	"./utils"
	"container/list"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/googollee/go-socket.io"
	"strings"
	"sync"
	_ "time"
)

type LogFilterFunc func(input []byte) string

type KafkaConsume struct {
	topic           string
	wsServer        *socketio.Server
	stopper         *utils.Stopper
	consumers       []sarama.PartitionConsumer
	masterConsumber sarama.Consumer
	filter          LogFilterFunc
}

func fileBeatLogFilter(input []byte) string {
	type FileBeatJson struct {
		Beat struct {
			Hostname string `json:"hostname"`
		} `json:"beat"`
		Message string `json:"message"`
	}
	var data FileBeatJson
	err := json.Unmarshal(input, &data)
	if err != nil {
		log.Printf("parser error %s\n", string(input))
		return "{\"error\":1}"
	}
	return string(data.Message)
}

func NewKafkaConsume(topic string, wsServer *socketio.Server, brokers []string, filterFunc LogFilterFunc) (*KafkaConsume, error) {
	s := &KafkaConsume{
		topic:     topic,
		wsServer:  wsServer,
		stopper:   utils.NewStopper(),
		consumers: make([]sarama.PartitionConsumer, 0, 8),
		filter:    filterFunc,
	}

	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	master, err := sarama.NewConsumer(brokers, config)
	if err != nil {
		return nil, err
	}
	s.masterConsumber = master

	//from partition 0
	/*
		consumer, err := master.ConsumePartition(topic, 0, sarama.OffsetNewest)
		s.consumers = append(s.consumers, consumer)
	*/
	partitionList, err := master.Partitions(topic)
	if err != nil {
		goto ERROR
	}

	for _, partition := range partitionList {
		consumer, err := master.ConsumePartition(topic, partition, sarama.OffsetNewest)
		if err != nil {
			goto ERROR
		}
		s.consumers = append(s.consumers, consumer)
	}

	for _, i := range s.consumers {
		log.Println(i)
	}

	return s, nil

ERROR:
	master.Close()
	for _, i := range s.consumers {
		i.Close()
	}
	return nil, err
}

func (kc *KafkaConsume) Start() {
	for _, partitionConsumer := range kc.consumers {
		kc.stopper.RunWorker(func() {
			for {
				select {
				case msg := <-partitionConsumer.Messages():
					kc.wsServer.BroadcastTo(kc.topic, "new_message", kc.filter(msg.Value))
				case err := <-partitionConsumer.Errors():
					log.Println(err)
					return
				case <-kc.stopper.ShouldStop():
					return
				default:
				}
			}
		})
	}
}

//block go routine
func (kc *KafkaConsume) Stop() {
	kc.stopper.Stop()
	kc.masterConsumber.Close()
	for _, i := range kc.consumers {
		i.Close()
	}
	fmt.Printf("kafka consume for topic %s is stopped\n", kc.topic)
}

func main() {

	/*
	*  topicMap and topicMapLock, topicUserList work together
	*  topicMap: topic => kafkaconsumer
	*  topicList: topic => [sid1, sid2, sid3, sid4]
	 */

	addr := flag.String("brokers", "127.0.0.1:9090", "broker address, seperated by comma")

	topicMap := make(map[string]*KafkaConsume)
	topicUserList := make(map[string]*list.List)
	var topicMapLock sync.Mutex

	server, err := socketio.NewServer(nil)
	if err != nil {
		log.Fatal(err)
	}

	flag.Parse()

	brokers := strings.Split(*addr, ",")

	leaveAllRoom := func(so socketio.Socket) {
		topicMapLock.Lock()
		defer topicMapLock.Unlock()

		for _, topic := range so.Rooms() {
			log.Println(topic)
			so.Leave(topic)
			//room name should be the topic name
			if _, ok := topicMap[topic]; ok {
				for e := topicUserList[topic].Front(); e != nil; e = e.Next() {
					if id, ok := e.Value.(string); ok && id == so.Id() {
						topicUserList[topic].Remove(e)
					}
				}
				if topicUserList[topic].Len() == 0 {
					topicMap[topic].Stop()
					delete(topicMap, topic)
				}
			}
		}
		log.Println("leave finished")
	}

	server.On("connection", func(so socketio.Socket) {
		log.Println("on connection")

		so.On("join", func(topic string) {

			//if ws client is in other rooms, leave the room first
			leaveAllRoom(so)

			log.Printf("%s is on join for %s", so.Id(), topic)
			so.Join(topic)

			topicMapLock.Lock()
			defer topicMapLock.Unlock()
			if _, ok := topicMap[topic]; ok {
				topicUserList[topic].PushBack(so.Id())
			} else {
				consumer, err := NewKafkaConsume(topic, server, brokers, fileBeatLogFilter)
				if err != nil {
					log.Println(err)
					return
				}
				topicMap[topic] = consumer
				consumer.Start()
				topicUserList[topic] = list.New()
				topicUserList[topic].PushBack(so.Id())
			}
		})

		so.On("leave", func(topic string) {
			leaveAllRoom(so)
		})

		so.On("disconnection", func() {
			leaveAllRoom(so)
			log.Printf("%s on disconnect\n", so.Id())
		})
	})
	server.On("error", func(so socketio.Socket, err error) {
		log.Println("error:", err)
	})
	http.Handle("/socket.io/", server)
	http.Handle("/", http.FileServer(http.Dir("./assets/")))
	log.Println("Serving at localhost:5000...")
	log.Fatal(http.ListenAndServe(":5000", nil))
}
