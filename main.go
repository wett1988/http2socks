package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func main() {
	localAddr := flag.String("listen", "127.0.0.1:8080", "HTTP proxy address and port")
	socks5Addr := flag.String("socks", "127.0.0.1:1080", "SOCKS5 proxy address and port")
	flag.Parse()

	ln, err := net.Listen("tcp", *localAddr)
	if err != nil {
		log.Fatalf("Couldn't listen %s: %v", *localAddr, err)
	}
	defer ln.Close()

	fmt.Printf("HTTP->SOCKS5 Converter is running.\n")
	fmt.Printf("HTTP is listening: %s\n", *localAddr)
	fmt.Printf("SOCKS5 server: %s\n", *socks5Addr)
	fmt.Println("Press Ctrl+C to stop.")

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Connection acceptance error: %v", err)
			continue
		}
		go handleClient(conn, *socks5Addr)
	}
}

func handleClient(client net.Conn, socks5Addr string) {
	defer client.Close()
	br := bufio.NewReader(client)

	reqLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	reqLine = strings.TrimSpace(reqLine)
	parts := strings.Fields(reqLine)
	if len(parts) < 3 {
		writeError(client, 400, "Bad Request")
		return
	}

	method := parts[0]
	target := ""

	if method == "CONNECT" {
		target = parts[1]
	} else {
		if u, err := url.Parse(parts[1]); err == nil && u.Host != "" {
			target = u.Host
		}
	}

	// Читаем заголовки
	var headers []string
	var hostFromHeader string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			writeError(client, 400, "Bad Request")
			return
		}
		headers = append(headers, line)
		if strings.TrimSpace(line) == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "host:") {
			hostFromHeader = strings.TrimSpace(line[5:])
		}
	}

	if target == "" {
		target = hostFromHeader
	}
	if target == "" {
		writeError(client, 400, "Host header missing")
		return
	}

	// Добавляем порт по умолчанию, если его нет
	target = ensurePort(target)
	host, portStr, _ := net.SplitHostPort(target)
	port, _ := strconv.Atoi(portStr)

	// Подключение к SOCKS5
	socksConn, err := net.Dial("tcp", socks5Addr)
	if err != nil {
		log.Printf("Error connecting to SOCKS5: %v", err)
		writeError(client, 502, "Bad Gateway")
		return
	}
	defer socksConn.Close()

	if err := socks5Handshake(socksConn); err != nil {
		log.Printf("SOCKS5 handshake error: %v", err)
		writeError(client, 502, "SOCKS5 Handshake Failed")
		return
	}

	if err := socks5Connect(socksConn, host, port); err != nil {
		log.Printf("Connection error via SOCKS5: %v", err)
		writeError(client, 502, "SOCKS5 Connect Failed")
		return
	}

	// Ответ клиенту
	if method == "CONNECT" {
		client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		log.Printf("Tunnel: %s", target)
	} else {
		client.Write([]byte(reqLine + "\r\n" + strings.Join(headers, "")))
		log.Printf("📄 HTTP request: %s", target)
	}

	// Двусторонний обмен данными
	go io.Copy(socksConn, client)
	io.Copy(client, socksConn)
}

func ensurePort(host string) string {
	if _, _, err := net.SplitHostPort(host); err != nil {
		return host + ":80"
	}
	return host
}

func writeError(c net.Conn, code int, msg string) {
	fmt.Fprintf(c, "HTTP/1.1 %d %s\r\n\r\n", code, msg)
}

// SOCKS5 Handshake (No Auth)
func socks5Handshake(conn net.Conn) error {
	_, err := conn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		return err
	}
	resp := make([]byte, 2)
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		return err
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		return fmt.Errorf("unsupported version/authentication: %v", resp)
	}
	return nil
}

// SOCKS5 CONNECT Command
func socks5Connect(conn net.Conn, host string, port int) error {
	var req []byte
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		ip4 := ip.To4()
		req = make([]byte, 10)
		req[0], req[1], req[2], req[3] = 0x05, 0x01, 0x00, 0x01
		copy(req[4:8], ip4)
		binary.BigEndian.PutUint16(req[8:10], uint16(port))
	} else {
		req = make([]byte, 7+len(host))
		req[0], req[1], req[2], req[3] = 0x05, 0x01, 0x00, 0x03
		req[4] = byte(len(host))
		copy(req[5:], host)
		binary.BigEndian.PutUint16(req[5+len(host):], uint16(port))
	}

	_, err := conn.Write(req)
	if err != nil {
		return err
	}

	header := make([]byte, 4)
	_, err = io.ReadFull(conn, header)
	if err != nil {
		return err
	}
	if header[0] != 0x05 || header[1] != 0x00 {
		return fmt.Errorf("SOCKS5 error: status %d", header[1])
	}

	// Пропускаем bound address/port
	switch header[3] {
	case 0x01:
		io.CopyN(io.Discard, conn, 6)
	case 0x03:
		var l uint8
		binary.Read(conn, binary.BigEndian, &l)
		io.CopyN(io.Discard, conn, int64(l)+2)
	case 0x04:
		io.CopyN(io.Discard, conn, 18)
	}
	return nil
}