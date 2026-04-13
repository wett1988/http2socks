# http2socks

A small proxy converter: accepts HTTP proxy connections locally and sends traffic through a SOCKS5 server. 

The program is cross-platform and works on both Linux and Windows.

 Supports:
- regular HTTP requests (for example, `GET http://example.com/...`);
- the `CONNECT` method for tunneling (usually used for HTTPS via HTTP proxy).

## How it works

1. The program listens to the TCP address of the local HTTP proxy.
2. When a new client connection is made, it parses the first line of the HTTP request and the headers.
3. It determines the target host/port.
4. It connects to the SOCKS5 server.
5. It performs a SOCKS5 handshake without authentication (`NO AUTH`).
6. It sends a SOCKS5 `CONNECT` to the target host and begins two-way data pumping.

## Launch parameters

- `-listen` — the address and port on which the local HTTP proxy is raised.  
  The default value is: `127.0.0.1:8080`
- `-socks` — the address and port of the SOCKS5 proxy through which the outgoing traffic goes.  
  The default value is: `127.0.0.1:1080`

## Launching

### Via `go run`

```bash
# for Linux
go run main.go -listen 127.0.0.1:8080 -socks 127.0.0.1:1080

# for Windows
GOOS=windows GOARCH=amd64 go run main.go -listen 127.0.0.1:8080 -socks 127.0.0.1:1080
```

### Building and running the binary

```bash
# for Linux
go build -o http2socks main.go
./http2socks -listen 127.0.0.1:8080 -socks 127.0.0.1:1080

# for Windows
GOOS=windows GOARCH=amd64 go build -o http2socks.exe main.go
./http2socks.exe -listen 127.0.0.1:8080 -socks 127.0.0.1:1080
```

After starting the program displays:
- the address of the local HTTP proxy (`HTTP is listening: ...`);
- the address of the SOCKS5 server (`SOCKS5 server: ...`).

## Example of use

Example of a request through a local HTTP proxy:

```bash
curl -x http://127.0.0.1:8080 http://example.com
```

## Restrictions

- SOCKS5 authentication is not supported (only `NO AUTH`);
- HTTP requests without an explicit port use port `80` by default.