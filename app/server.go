package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"os/signal"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var directory = flag.String("directory", "", "The directory to use")

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
	method := fields[0]

	if urlPath[1] == "" {
		conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
	} else if urlPath[1] == "echo" {
		if len(header["Accept-Encoding"]) > 0 {
			idx := slices.IndexFunc(strings.Split(header["Accept-Encoding"][0], ", "), func(c string) bool { return c == "gzip" })
			if idx != -1 {
				m := urlPath[2]
				gzipBytes := new(bytes.Buffer)
				gz := gzip.NewWriter(gzipBytes)
				if _, err := gz.Write([]byte(m)); err != nil {
					res := fmt.Sprintf("HTTP/1.1 500 Internal Server Error\r\n" + "\r\n")
					conn.Write([]byte(res))
					conn.Close()
					return
				}
				if err := gz.Close(); err != nil {
					res := fmt.Sprintf("HTTP/1.1 500 Internal Server Error\r\n" + "\r\n")
					conn.Write([]byte(res))
					conn.Close()
					return
				}
				res := fmt.Sprintf("HTTP/1.1 200 OK\r\n"+"Content-Encoding: gzip\r\n"+"Content-Type: text/plain\r\n"+"Content-Length: %d\r\n\r\n", gzipBytes.Len())
				conn.Write([]byte(res))
				conn.Write(gzipBytes.Bytes())
				conn.Close()
				return
			}
		}
		m := urlPath[2]
		res := fmt.Sprintf("HTTP/1.1 200 OK\r\n"+"Content-Type: text/plain\r\n"+"Content-Length: %d\r\n"+"\r\n"+"%s", len(m), m)
		conn.Write([]byte(res))
		conn.Close()

	} else if urlPath[1] == "user-agent" {
		m := header.Get("User-Agent")
		res := fmt.Sprintf("HTTP/1.1 200 OK\r\n"+"Content-Type: text/plain\r\n"+"Content-Length: %d\r\n"+"\r\n"+"%s", len(m), m)
		conn.Write([]byte(res))
		conn.Close()
	} else if urlPath[1] == "files" {
		if *directory == "" {
			res := fmt.Sprintf("HTTP/1.1 500 Internal Server Error\r\n" + "\r\n")
			conn.Write([]byte(res))
			conn.Close()
			return
		}
		fileName := urlPath[2]
		pathToFile := path.Join(*directory, fileName)
		if method == "GET" {
			data, err := os.ReadFile(pathToFile)
			if err != nil && errors.Is(err, os.ErrNotExist) {
				res := fmt.Sprintf("HTTP/1.1 404 Not Found\r\n" + "\r\n")
				conn.Write([]byte(res))
				conn.Close()
				return
			}

			res := fmt.Sprintf("HTTP/1.1 200 OK\r\n"+"Content-Type: application/octet-stream\r\n"+"Content-Length: %d\r\n"+"\r\n", len(data))
			conn.Write([]byte(res))
			conn.Write(data)
			conn.Close()
		}
		if method == "POST" {
			requestBody, err := io.ReadAll(tp.R)
			if err != nil {
				fmt.Println("Error reading request body:", err.Error())
				return
			}
			file, err := os.Create(pathToFile)
			if err != nil {
				fmt.Println("Error creating file:", err.Error())
				return
			}
			length, err := strconv.Atoi(header["Content-Length"][0])
			if err != nil {
				fmt.Println("Error reading content-length:", err.Error())
				return
			}
			file.Write(requestBody[:length])
			file.Close()
			res := fmt.Sprintf("HTTP/1.1 201 Created\r\n\r\n")
			conn.Write([]byte(res))
			conn.Close()
		}

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
	flag.Parse()

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
