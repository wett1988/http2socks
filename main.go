package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxRequestLine = 8 << 10
	maxHeaderBytes = 64 << 10
)

func main() {
	localAddr := flag.String("listen", "127.0.0.1:8080", "HTTP proxy address and port")
	socks5Addr := flag.String("socks", "127.0.0.1:1080", "SOCKS5 proxy address and port")
	bypassList := flag.String("bypass", "", "Comma-separated list of hosts to bypass the SOCKS5 proxy")
	proxiedList := flag.String("proxied", "", "Comma-separated list of hosts to route through the SOCKS5 proxy (others go direct)")
	flag.Parse()

	bypass := parseBypass(*bypassList)
	proxied := parseBypass(*proxiedList)

	if len(bypass) > 0 && len(proxied) > 0 {
		log.Fatal("use either -bypass or -proxied, not both")
	}

	ln, err := net.Listen("tcp", *localAddr)
	if err != nil {
		log.Fatalf("Couldn't listen %s: %v", *localAddr, err)
	}
	defer ln.Close()

	fmt.Printf("HTTP->SOCKS5 Converter is running.\n")
	fmt.Printf("HTTP is listening: %s\n", *localAddr)
	fmt.Printf("SOCKS5 server: %s\n", *socks5Addr)
	if len(bypass) > 0 {
		fmt.Printf("Bypass list: %s\n", strings.Join(bypass, ", "))
	}
	if len(proxied) > 0 {
		fmt.Printf("Proxied list: %s\n", strings.Join(proxied, ", "))
	}
	fmt.Println("Press Ctrl+C to stop.")

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("Connection acceptance error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go handleClient(conn, *socks5Addr, bypass, proxied)
	}
}

func handleClient(client net.Conn, socks5Addr string, bypass, proxied []string) {
	defer client.Close()
	br := bufio.NewReader(client)

	reqLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	if len(reqLine) > maxRequestLine {
		writeError(client, 431, "Request Header Fields Too Large")
		return
	}
	reqLine = strings.TrimSpace(reqLine)
	parts := strings.Fields(reqLine)
	if len(parts) < 3 {
		writeError(client, 400, "Bad Request")
		return
	}

	method := parts[0]
	proto := parts[2]
	target := ""
	reqTarget := ""
	defaultPort := 80

	if method == "CONNECT" {
		target = parts[1]
		defaultPort = 443
	} else {
		if u, err := url.Parse(parts[1]); err == nil && u.Host != "" {
			target = u.Host
			if u.Scheme == "https" || u.Scheme == "wss" {
				defaultPort = 443
			}
			if idx := strings.Index(parts[1], u.Host); idx >= 0 {
				reqTarget = parts[1][idx+len(u.Host):]
			} else {
				reqTarget = u.RequestURI()
			}
		} else {
			reqTarget = parts[1]
		}
	}

	if reqTarget == "" {
		reqTarget = "/"
	} else if reqTarget[0] != '/' {
		reqTarget = "/" + reqTarget
	}

	// Читаем заголовки
	var headers []string
	var hostFromHeader string
	headerBytes := 0
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			writeError(client, 400, "Bad Request")
			return
		}
		headerBytes += len(line)
		if headerBytes > maxHeaderBytes {
			writeError(client, 431, "Request Header Fields Too Large")
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
	target = ensurePort(target, defaultPort)
	host, portStr, _ := net.SplitHostPort(target)
	port, _ := strconv.Atoi(portStr)

	var upstream net.Conn

	useProxy := true
	if len(proxied) > 0 {
		useProxy = isBypassed(host, proxied)
	} else if len(bypass) > 0 {
		useProxy = !isBypassed(host, bypass)
	}

	if !useProxy {
		upstream, err = net.Dial("tcp", target)
		if err != nil {
			log.Printf("Direct connection error: %v", err)
			writeError(client, 502, "Bad Gateway")
			return
		}
		log.Printf("Bypass (direct): %s", target)
	} else {
		upstream, err = net.Dial("tcp", socks5Addr)
		if err != nil {
			log.Printf("Error connecting to SOCKS5: %v", err)
			writeError(client, 502, "Bad Gateway")
			return
		}

		if err := socks5Handshake(upstream); err != nil {
			log.Printf("SOCKS5 handshake error: %v", err)
			writeError(client, 502, "SOCKS5 Handshake Failed")
			return
		}

		if err := socks5Connect(upstream, host, port); err != nil {
			log.Printf("Connection error via SOCKS5: %v", err)
			writeError(client, 502, "SOCKS5 Connect Failed")
			return
		}
	}
	defer upstream.Close()

	// Ответ клиенту
	if method == "CONNECT" {
		client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		log.Printf("Tunnel: %s", target)
	} else {
		upstream.Write([]byte(method + " " + reqTarget + " " + proto + "\r\n" + strings.Join(headers, "")))
		log.Printf("HTTP request: %s", target)
	}

	// Двусторонний обмен данными
	go io.Copy(upstream, br)
	io.Copy(client, upstream)
}

func parseBypass(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func isBypassed(host string, patterns []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, p := range patterns {
		if strings.HasPrefix(p, "*.") {
			base := p[2:]
			if host == base || strings.HasSuffix(host, "."+base) {
				return true
			}
		} else if host == p || strings.HasSuffix(host, "."+p) {
			return true
		}
	}
	return false
}

func ensurePort(host string, defaultPort int) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	h := strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return net.JoinHostPort(h, strconv.Itoa(defaultPort))
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
	switch {
	case ip != nil && ip.To4() != nil:
		ip4 := ip.To4()
		req = make([]byte, 10)
		req[0], req[1], req[2], req[3] = 0x05, 0x01, 0x00, 0x01
		copy(req[4:8], ip4)
		binary.BigEndian.PutUint16(req[8:10], uint16(port))
	case ip != nil:
		ip16 := ip.To16()
		req = make([]byte, 22)
		req[0], req[1], req[2], req[3] = 0x05, 0x01, 0x00, 0x04
		copy(req[4:20], ip16)
		binary.BigEndian.PutUint16(req[20:22], uint16(port))
	default:
		if len(host) > 255 {
			return fmt.Errorf("hostname too long: %d bytes", len(host))
		}
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
