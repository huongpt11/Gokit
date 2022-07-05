FROM golang:1.18

WORKDIR /usr/src/test

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
# COPY go.mod go.sum ./
COPY . .
RUN go mod download && go mod verify

RUN go build -v -o /usr/local/bin/test ./main.go

CMD ["/usr/local/bin/test"]