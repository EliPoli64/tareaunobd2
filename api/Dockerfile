FROM golang:1.27.0-alpine3.24

# apoyado de https://docs.docker.com/guides/golang/

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /t1bd2

CMD ["/t1bd2"]