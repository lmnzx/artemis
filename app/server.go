package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type server struct {
	wg         sync.WaitGroup
	listener   net.Listener
	shutdown   chan struct{}
	connection chan net.Conn
}

func newServer(address string) (*server, error) {
	listener, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		return nil, fmt.Errorf("failed to listen on address %s: %w", address, err)
	}

	return &server{
		listener:   listener,
		shutdown:   make(chan struct{}),
		connection: make(chan net.Conn),
	}, nil
}

func (s *server) acceptConnections() {
	defer s.wg.Done()

	for {
		select {
		case <-s.shutdown:
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				continue
			}
			s.connection <- conn
		}
	}
}

func (s *server) handleConnections() {
	defer s.wg.Done()

	for {
		select {
		case <-s.shutdown:
			return
		case conn := <-s.connection:
			go s.handleConnection(conn)
		}
	}
}

func (s *server) handleConnection(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)
	_, err := conn.Read(buf)
	if err != nil {
		fmt.Println("Error reading: ", err.Error())
		return
	}

	reader := bufio.NewReader(bytes.NewReader(buf))
	tp := textproto.NewReader(reader)
	requestLine, err := tp.ReadLine()
	if err != nil {
		fmt.Println("Error reading: ", err.Error())
		return
	}

	header, err := tp.ReadMIMEHeader()
	if err != nil {
		fmt.Println("Error reading: ", err.Error())
		return
	}

	fields := strings.Split(string(requestLine), " ")
	if len(fields) < 2 {
		fmt.Printf("invalid request line: %s\n", string(requestLine))
		return
	}
	urlPath := strings.Split(fields[1], "/")

	if urlPath[1] == "" {
		conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
	} else if urlPath[1] == "echo" {
		m := urlPath[2]
		res := fmt.Sprintf("HTTP/1.1 200 OK\r\n"+"Content-Type: text/plain\r\n"+"Content-Length: %d\r\n"+"\r\n"+"%s", len(m), m)
		conn.Write([]byte(res))
		conn.Close()

	} else if urlPath[1] == "user-agent" {
		m := header.Get("User-Agent")
		res := fmt.Sprintf("HTTP/1.1 200 OK\r\n"+"Content-Type: text/plain\r\n"+"Content-Length: %d\r\n"+"\r\n"+"%s", len(m), m)
		conn.Write([]byte(res))
		conn.Close()
	} else {
		conn.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\n"))
		conn.Close()
	}
}

func (s *server) Start() {
	s.wg.Add(2)
	go s.acceptConnections()
	go s.handleConnections()
}

func (s *server) Stop() {
	close(s.shutdown)
	s.listener.Close()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return
	case <-time.After(time.Second):
		fmt.Println("Timed out waiting for connections to finish.")
		return
	}
}

func main() {
	s, err := newServer(":4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}

	s.Start()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("Shutting down server...")
	s.Stop()
	fmt.Println("Server stopped.")
}
