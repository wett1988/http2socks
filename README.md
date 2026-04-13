# http2socks

A small proxy converter: accepts HTTP proxy connections locally and sends traffic through a SOCKS5 server.

Supports:

- regular HTTP requests (for example, `GET http://example.com/...`);
- the `CONNECT` method for tunneling (usually used for HTTPS via an HTTP proxy).

## How it works

1. The program listens on the TCP address of the local HTTP proxy.
2. Parses the first line of the HTTP request and headers on a new client connection.
3. Determines the target host/port.
4. Connects to the SOCKS5 server.
5. Performs a SOCKS5 handshake without authentication (`NO AUTH`).
6. Sends a SOCKS5 `CONNECT` to the target host and starts two-way data pumping.

## Launch options

- `-listen` — address and port on which the local HTTP proxy is raised.  
  Default value: `127.0.0.1:8080`
- `-socks` — address and port of the SOCKS5 proxy through which the outgoing traffic goes.  
  Default value: `127.0.0.1:1080`  

## Launching  

### Via `go run`  
```bash
go run main.go -listen 127.0.0.1:8080 -socks 127.0.0.1:1080
```

### Building and running the binary

```bash
go build -o http2socks main.go
./http2socks -listen 127.0.0.1:8080 -socks 127.0.0.1:1080
```

After starting, the program displays:
- the address of the local HTTP proxy (`HTTP is listening: ...`);
- the address of the SOCKS5 server (`SOCKS5 server: ...`).

## Example usage

Example of a request through a local HTTP proxy:

```bash
curl -x http://127.0.0.1:8080 http://example.com
```

## Limitations

- SOCKS5 authentication is not supported (only `NO AUTH`);
- HTTP requests without an explicit port use port `80` by default.