FROM golang:1.9.2

ADD . /go/src/github.com/anteater2/bitmesh-chat

RUN go get github.com/anteater2/bitmesh-chat