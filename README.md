# Go Proxy

## About

This project was made as a simple exercise/side-project and
to help debug/inspect some services like AWS DynamoDB to see
how the SDK communicates with the server so that to be able
to mock it. It's not a serious project.

### Logs

The proxy logs the requests made to the target server. The logs
are stored in the `logs` folder with a file whose name is the host
of the target server and its port (if any) separated by a dot. For
example: `logs/some-server` or `logs/some-other-server.8888`

## Usage

```shell
git clone https://github.com/pedrothome1/go-proxy.git
cd go-proxy
go build .
./go-proxy -p 8081 -addr https://some-server
```

### Parameters

```
-addr string
    The server address (scheme://host) to forward the request to
-p int
    The TCP port to bind the server to (default 8080)
```
