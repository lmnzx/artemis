package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"strings"
)

func main() {
	l, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			os.Exit(1)
		}

		go func(conn net.Conn) {
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

			} else if urlPath[1] == "user-agent" {
				m := header.Get("User-Agent")
				res := fmt.Sprintf("HTTP/1.1 200 OK\r\n"+"Content-Type: text/plain\r\n"+"Content-Length: %d\r\n"+"\r\n"+"%s", len(m), m)
				conn.Write([]byte(res))
			} else {
				conn.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\n"))
			}

			conn.Close()
		}(conn)

	}
}
