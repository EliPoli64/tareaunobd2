FROM golang:1.27.0-alpine3.24 AS builder

# apoyado de https://docs.docker.com/guides/golang/

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -o /t1bd2

FROM alpine:3.24
COPY --from=builder /t1bd2 /t1bd2
EXPOSE 1412
CMD ["/t1bd2"]