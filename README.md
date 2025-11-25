# Illustration2 - Gin HTTP Server

This is a simple HTTP server built with the Gin framework in Go.

## Prerequisites

- Go 1.16 or later

## Getting Started

### 1. Initialize the module

```bash
go mod init illustration2
```

### 2. Install dependencies

```bash
go get github.com/gin-gonic/gin
```

### 3. Run the server

```bash
go run main.go
```

The server will start and listen on http://localhost:8080

### 4. Test the server

```bash
curl http://localhost:8080
```

You should see the response:
```json
{"message":"Hello from Gin HTTP server!"}
```

## Project Structure

- `main.go`: The main server file
- `go.mod`: Go module file
- `go.sum`: Go dependencies file

## Features

- Simple GET endpoint at / that returns a JSON response
- Gin default middleware (Logger and Recovery)

## License

MIT
