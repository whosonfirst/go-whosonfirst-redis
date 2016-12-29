package pubsub

// https://redis.io/topics/protocol
// https://redis.io/topics/pubsub
// https://www.redisgreen.net/blog/beginners-guide-to-redis-protocol/
// https://www.redisgreen.net/blog/reading-and-writing-redis-protocol/

import (
	"errors"
	"fmt"
	"github.com/whosonfirst/go-redis-tools/resp"
	"log"
	"net"
	"strings"
)

type Clients map[string]bool

type Server struct {
	host  string
	port  int
	subs  map[string]Clients
	conns map[string]net.Conn
}

func NewServer(host string, port int) (*Server, error) {

	conns := make(map[string]net.Conn)
	subs := make(map[string]Clients)

	s := Server{
		host:  host,
		port:  port,
		subs:  subs,
		conns: conns,
	}

	return &s, nil
}

func (s *Server) ListenAndServe() error {

	address := fmt.Sprintf("%s:%d", s.host, s.port)
	daemon, err := net.Listen("tcp", address)

	if err != nil {
		return err
	}

	defer daemon.Close()

	for {

		conn, err := daemon.Accept()

		if err != nil {
			return err
		}

		log.Printf("hello %s", conn.RemoteAddr())
		go s.Receive(conn)
	}

	return nil
}

func (s *Server) Receive(conn net.Conn) {

	reader := resp.NewRESPReader(conn)
	writer := resp.NewRESPWriter(conn)

	raw, err := reader.ReadObject()

	if err != nil {
		log.Println(err)
		conn.Close()
	}

	body := strings.Split(string(raw), "\r\n")

	if len(body) == 0 {
		conn.Close()
	}

	cmd := body[2]

	if cmd == "SUBSCRIBE" {

		channels := body[1:]
		rsp, err := s.Subscribe(conn, channels)

		if err != nil {
			writer.WriteError(err)
			conn.Close()
		}

		writer.WriteArray(rsp)

	} else if cmd == "UNSUBSCRIBE" {

		channels := body[1:]
		rsp, err := s.Unsubscribe(conn, channels)

		if err != nil {
			writer.WriteError(err)
			conn.Close()
		}

		writer.WriteArray(rsp)
		conn.Close()

	} else if cmd == "PUBLISH" {

		channel := body[1]
		msg := strings.Join(body[2:], " ")
		rsp, err := s.Publish(channel, []byte(msg))

		if err != nil {
			writer.WriteError(err)
			conn.Close()
		}

		writer.WriteArray(rsp)

	} else if cmd == "PING" {

		writer.WriteSingle("PONG")
		conn.Close()

	} else {

		msg := fmt.Sprintf("unknown command '%s'", cmd)
		err := errors.New(msg)

		writer.WriteError(err)
		conn.Close()
	}
}

func (s *Server) Subscribe(conn net.Conn, channels []string) ([]string, error) {

	rsp := make([]string, 0)

	remote := conn.RemoteAddr().String()

	_, ok := s.conns[remote]

	if !ok {
		s.conns[remote] = conn
	}

	for _, ch := range channels {

		clients, ok := s.subs[ch]

		if !ok {
			clients = make(map[string]bool)
			s.subs[ch] = clients
		}

		s.subs[ch][remote] = true
	}

	return rsp, nil
}

func (s *Server) Unsubscribe(conn net.Conn, channels []string) ([]string, error) {

	rsp := make([]string, 0)

	remote := conn.RemoteAddr().String()

	_, ok := s.conns[remote]

	if !ok {
		msg := fmt.Sprintf("Can not find connection thingy for %s", remote)
		err := errors.New(msg)
		return rsp, err
	}

	for _, ch := range channels {

		_, ok := s.subs[ch]

		if !ok {
			continue
		}

		_, ok = s.subs[ch][remote]

		if !ok {
			continue
		}

		delete(s.subs[ch], remote)
	}

	count := 0

	for _, ch := range channels {

		for addr, _ := range s.subs[ch] {

			if addr == remote {
				count += 1
			}
		}
	}

	if count == 0 {
		delete(s.conns, remote)
	}

	return rsp, nil
}

func (s *Server) Publish(channel string, message []byte) ([]string, error) {

	rsp := make([]string, 0)

	sub, ok := s.subs[channel]

	if !ok {
		return rsp, nil
	}

	for remote, _ := range sub {

		conn, ok := s.conns[remote]

		if !ok {
			continue
		}

		go func(c net.Conn, m string) {

			rsp := []string{m}

			writer := resp.NewRESPWriter(c)
			writer.WriteArray(rsp)

		}(conn, string(message))

	}

	return rsp, nil
}
